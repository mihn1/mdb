package db

import (
	"sync"

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
	opts       *Options
}

// Options contains configuration for the database
type Options struct {
	MemTableSize uint64 // Maximum size of MemTable before flushing to disk
}

// Open a database at the given path
func Open(path string, opts *Options) (*DB, error) {
	// TODO: Implement database opening logic
	db := &DB{
		memtable:   memtable.New(),
		immutable:  nil,
		memtableMu: sync.RWMutex{},
		path:       path,
		opts:       opts,
	}
	// TODO: Load existing SSTables and WAL if any
	return db, nil
}

func (db *DB) Put(key, value []byte) error {
	// Write to the MemTable for now
	err := db.memtable.Put(key, value)
	if err != nil {
		return err
	}

	// Check if we need to flush the memtable
	if db.shouldFlush() {
		// In a real implementation, we would handle the immutable MemTable and WAL here
		// For now, just simulate a flush
		// The flushing should be done in a separate goroutine to avoid blocking
		go db.flush()
	}

	return nil
}

func (db *DB) Get(key []byte) ([]byte, error) {
	// Get in the MemTable for now
	value, found := db.memtable.Get(key)
	if !found {
		return nil, nil // Key not found
	}
	return value, nil
}

func (db *DB) Delete(key []byte) error {
	// Delete in the MemTable for now
	_, err := db.memtable.Delete(key)
	return err
}

func (db *DB) Close() error {
	// TODO: Implement cleanup
	return nil
}

func (db *DB) shouldFlush() bool {
	return db.memtable.Size() >= db.opts.MemTableSize
}

func (db *DB) flush() error {
	// TODO: Implement flush logic
	db.memtableMu.Lock()
	defer db.memtableMu.Unlock()

	newMemtable := memtable.New()
	db.immutable = db.memtable
	db.memtable = newMemtable
	// Now memtable is free to be flushed to disk asynchronously
	// For now, just simulate the flush with a sleep or log
	// In a real implementation, we would write the immutable memtable to an SSTable on disk

	err := sstable.BuildTable(db.immutable, db.path)
	utils.AssertNoErr(err, "failed to build sstable from memtable: %v") // Should panic if critical error occurs during flush
	return err
}
