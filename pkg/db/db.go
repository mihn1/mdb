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
	// flushCond  *sync.Cond   // Condition variable for flushing
	flushMu   sync.Mutex
	path      string
	opts      *common.Options
	flushes   uint64 // number of successful flushes
	globalSeq uint64 // global sequence number for SSTable files
}

// Open a database at the given path
func Open(path string, opts *common.Options) (*DB, error) {
	db := &DB{
		memtable:   memtable.New(opts),
		immutable:  nil,
		memtableMu: sync.RWMutex{},
		// flushCond:  sync.NewCond(&sync.Mutex{}),
		flushMu: sync.Mutex{},
		path:    path,
		opts:    opts,
	}
	// TODO: Load existing SSTables and WAL if any
	return db, nil
}

func (db *DB) Put(key, value []byte) error {
	// Lock for reading to allow concurrent reads but block during flush
	db.memtableMu.RLock()
	defer db.memtableMu.RUnlock()

	// TODO: add WAL write before memtable update
	err := db.memtable.Put(key, value)
	if err != nil {
		return err
	}

	// Schedule a flush if needed
	go db.maybeFlush(false)

	return nil
}

func (db *DB) Get(key []byte) ([]byte, error) {
	// Check memtable first
	value, found := db.memtable.Get(key)
	if found {
		return unboxValue(value), nil
	}

	// Check immutable memtable if it exists
	if db.immutable != nil {
		value, found = db.immutable.Get(key)
		if found {
			return unboxValue(value), nil
		}
	}

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
	// Delete in the MemTable for now
	return db.memtable.Delete(key)
}

func (db *DB) Close() error {
	// TODO: Implement cleanup
	// Flush any remaining memtable
	if db.opts.EnableDebugLogging {
		log.Printf("Closing DB, flushing memtable if needed")
		stat := db.Stats()
		log.Printf("Before Close: Flushes %d memtable at %d", stat.Flushes, stat.MemTableSize)
	}

	if err := db.maybeFlush(true); err != nil {
		return err
	}

	return nil
}

func (db *DB) maybeFlush(force bool) error {
	db.flushMu.Lock()
	defer db.flushMu.Unlock()
	db.memtableMu.Lock()

	size := db.memtable.Size()
	if db.immutable != nil || // Another flush is in progress
		size == 0 ||
		(size < db.opts.MemTableSize && !force) {
		db.memtableMu.Unlock()
		return nil
	}

	if db.opts.EnableDebugLogging {
		stat := db.Stats()
		log.Printf("Flushes %d memtable at %d", stat.Flushes, stat.MemTableSize)
	}

	db.immutable = db.memtable
	db.memtable = memtable.New(db.opts)
	db.memtableMu.Unlock() // Unlock memtable for reading after swapping

	err := db.flushMemtable(db.immutable)
	db.immutable = nil

	if err != nil {
		return err
	}

	db.flushes++
	return nil
}

// Stats holds simple debug counters (unstable API).
type Stats struct {
	MemTableSize uint64
	Flushes      uint64
}

// Stats returns current debug counters.
func (db *DB) Stats() Stats {
	return Stats{
		MemTableSize: db.memtable.Size(),
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
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	builder, err := sstable.NewTableBuilder(file, db.opts)
	if err != nil {
		return err
	}
	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		value, err := iter.Value()
		if err != nil {
			return err
		}

		if err := builder.Add(key, value); err != nil {
			return err
		}
	}
	return builder.Finish()
}

func (db *DB) getTablesDir() string {
	dir := filepath.Join(db.path, "tables")
	utils.CreateDirIfNotExist(dir)
	return dir
}
