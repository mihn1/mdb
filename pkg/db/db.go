package db

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mihn1/mdb/pkg/common"
	"github.com/mihn1/mdb/pkg/memtable"
	"github.com/mihn1/mdb/pkg/sstable"
	"github.com/mihn1/mdb/pkg/utils"
)

// DB represents the main database interface
type DB struct {
	// TODO: Add fields for MemTable, WAL, SSTables, etc.
	memtable   *memtable.MemTable
	immutable  *memtable.MemTable
	memtableMu sync.RWMutex // Mutex to protect memtable access during swaps
	flushCond  *sync.Cond   // Condition variable for coordinating flushes
	path       string
	opts       *common.Options
	flushes    uint64 // number of successful flushes
	globalSeq  uint64 // global sequence number for SSTable files
	flushErr   error  // last flush error encountered
}

// Open a database at the given path
func Open(path string, opts *common.Options) (*DB, error) {
	db := &DB{
		memtable:   memtable.New(opts),
		immutable:  nil,
		memtableMu: sync.RWMutex{},
		path:       path,
		opts:       opts,
	}
	db.flushCond = sync.NewCond(&db.memtableMu)
	// TODO: Load existing SSTables and WAL if any
	return db, nil
}

func (db *DB) Put(key, value []byte) error {
	db.memtableMu.Lock()
	// TODO: add WAL write before memtable update
	err := db.memtable.Put(key, value)
	db.memtableMu.Unlock()
	if err != nil {
		return err
	}

	return db.maybeFlush(false)
}

func (db *DB) Get(key []byte) ([]byte, error) {
	db.memtableMu.RLock()
	// Capture memtable reference for safe access after unlock
	mem := db.memtable
	imm := db.immutable
	db.memtableMu.RUnlock()

	// Check memtable first
	value, found := mem.Get(key)
	if found {
		return unboxValue(value), nil
	}

	// Check immutable memtable if it exists
	if imm != nil {
		value, found = imm.Get(key)
		if found {
			return unboxValue(value), nil
		}
	}

	// No need to lock when reading from SSTables
	// Keep finding in SSTables
	tablesDir := db.getTablesDir()
	// Check all tables sorted by newest first
	entries, err := os.ReadDir(tablesDir)
	if err != nil {
		return nil, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".sst" {
			continue
		}
		filePath := filepath.Join(tablesDir, entry.Name())
		file, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		table, err := sstable.Open(file, db.opts)
		if err != nil {
			file.Close()
			return nil, err
		}
		value, err = table.Get(key)
		file.Close()
		if err != nil {
			return nil, err
		}
		if value != nil {
			return unboxValue(value), nil
		}
	}

	// Not found in any SSTable
	return nil, nil
}

func unboxValue(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}

	// Values are stored with a type marker as the last byte
	lastByte := value[len(value)-1]
	if lastByte == byte(common.TypeValue) {
		// Strip the type marker and return the actual value
		return value[:len(value)-1]
	} else if lastByte == byte(common.TypeTombstone) {
		return nil // This is a tombstone (deleted key)
	}

	// If no recognized type marker, return as-is (shouldn't happen in normal operation)
	return value
}

func (db *DB) Delete(key []byte) error {
	db.memtableMu.Lock()
	value := []byte{byte(common.TypeTombstone)}
	err := db.memtable.Put(key, value)
	db.memtableMu.Unlock()
	if err != nil {
		return err
	}
	return db.maybeFlush(false)
}

func (db *DB) Close() error {
	// TODO: Implement cleanup
	// Flush any remaining memtable
	if db.opts.EnableDebugLogging {
		log.Printf("Closing DB, flushing memtable if needed")
		stat := db.Stats()
		log.Printf("Before Close: Flushes %d memtable at %d", stat.Flushes, stat.MemTableSize)
	}

	return db.maybeFlush(true)
}

func (db *DB) maybeFlush(force bool) error {
	db.memtableMu.Lock()

	for db.immutable != nil {
		if db.flushErr != nil {
			err := db.flushErr
			db.memtableMu.Unlock()
			return err
		}
		db.flushCond.Wait()
	}

	size := db.memtable.Size()
	if size == 0 || (size < db.opts.MemTableSize && !force) {
		db.memtableMu.Unlock()
		return nil
	}

	if db.opts.EnableDebugLogging {
		log.Printf("Flushing memtable: current size %d", size)
	}

	imm := db.memtable
	db.immutable = imm
	db.memtable = memtable.New(db.opts)
	db.memtableMu.Unlock()

	// Release the lock while flushing since the cond var already protects flushing
	err := db.flushMemtable(imm)

	db.memtableMu.Lock()
	if err != nil {
		db.flushErr = err
		db.flushCond.Broadcast()
		db.memtableMu.Unlock()
		return err
	}

	db.immutable = nil
	db.flushErr = nil
	atomic.AddUint64(&db.flushes, 1)
	db.flushCond.Broadcast()
	db.memtableMu.Unlock()

	return nil
}

// Stats holds simple debug counters (unstable API).
type Stats struct {
	MemTableSize uint64
	Flushes      uint64
}

// Stats returns current debug counters.
func (db *DB) Stats() Stats {
	db.memtableMu.RLock()
	size := db.memtable.Size()
	db.memtableMu.RUnlock()
	return Stats{
		MemTableSize: size,
		Flushes:      atomic.LoadUint64(&db.flushes),
	}
}

func (db *DB) flushMemtable(mem *memtable.MemTable) error {
	// iterate over memtable
	// write to sstable file
	iter := mem.Iterator()
	utils.AssertMsg(iter != nil, "memtable iterator should not be nil")

	dir := db.getTablesDir()
	seq := atomic.AddUint64(&db.globalSeq, 1) - 1
	ts := time.Now().UnixNano()
	filePath := filepath.Join(dir, fmt.Sprintf("sstable-%d-%d-%d.sst", 0, seq, ts))
	tmpPath := filePath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	builder, err := sstable.NewTableBuilder(file, db.opts)
	if err != nil {
		file.Close()
		os.Remove(tmpPath)
		return err
	}
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			file.Close()
			os.Remove(tmpPath)
			return err
		}
		value, err := iter.Value()
		if err != nil {
			file.Close()
			os.Remove(tmpPath)
			return err
		}

		if err := builder.Add(key, value); err != nil {
			file.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := builder.Finish(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, filePath)
}

func (db *DB) getTablesDir() string {
	dir := filepath.Join(db.path, "tables")
	utils.CreateDirIfNotExist(dir)
	return dir
}
