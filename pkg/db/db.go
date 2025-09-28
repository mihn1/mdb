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
	// TODO: Implement database opening logic

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
	return db.memtable.Delete(key)
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

	newMemtable := memtable.New(db.opts)
	db.immutable = db.memtable
	db.memtable = newMemtable
	// Now memtable is free to be flushed to disk asynchronously
	// For now, just simulate the flush with a sleep or log
	// In a real implementation, we would write the immutable memtable to an SSTable on disk

	err := db.flushMemtable(db.immutable, db.path)
	utils.AssertNoErr(err, "failed to build sstable from memtable: %v") // panic on critical error
	if err == nil {
		atomic.AddUint64(&db.flushes, 1)
	}
	return err
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

func (db *DB) flushMemtable(mem *memtable.MemTable, path string) error {
	// iterate over memtable
	// write to sstable file
	iter := mem.Iterator()
	utils.AssertMsg(iter != nil, "memtable iterator should not be nil")

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	dir := filepath.Join(path, "tables")
	utils.CreateDirIfNotExist(dir)
	seq := atomic.AddUint64(&db.globalSeq, 1) - 1
	ts := time.Now().UnixNano()
	filePath := filepath.Join(dir, fmt.Sprintf("sstable-%d-%d-%d.sst", 0, seq, ts))
	file, err = os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	builder, err := sstable.NewTableBuilder(file, nil)
	if err != nil {
		return err
	}
	for ; iter.Valid(); iter.Next() {
		if err := builder.Add(iter.Key(), iter.Value()); err != nil {
			return err
		}
	}
	return builder.Finish()
}
