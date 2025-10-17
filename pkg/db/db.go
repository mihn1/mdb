package db

import (
	"fmt"
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
	memtableMu sync.RWMutex // Mutex to protect memtable access during flush and swaps
	path       string
	opts       *common.Options
	flushes    uint64 // number of successful flushes
	globalSeq  uint64 // global sequence number for SSTable files
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
	// TODO: Load existing SSTables and WAL if any
	return db, nil
}

func (db *DB) Put(key, value []byte) error {
	// TODO: add WAL write before memtable update
	err := db.memtable.Put(key, value)
	if err != nil {
		return err
	}

	// Check if we need to flush the memtable
	if db.shouldFlush() {
		// The flushing should be done in a separate goroutine to avoid blocking
		go db.flush()
	}

	return nil
}

func (db *DB) Get(key []byte) ([]byte, error) {
	// Get in the MemTable for now
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
	if db.memtable.Size() > 0 {
		if err := db.flush(); err != nil {
			return err
		}
	}

	return nil
}

func (db *DB) shouldFlush() bool {
	return db.memtable.Size() >= db.opts.MemTableSize
}

func (db *DB) flush() error {
	db.memtableMu.Lock()
	defer db.memtableMu.Unlock()

	db.immutable = db.memtable
	db.memtable = memtable.New(db.opts)

	err := db.flushMemtable(db.immutable)
	if err != nil {
		return err
	}
	db.immutable = nil
	atomic.AddUint64(&db.flushes, 1)
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
