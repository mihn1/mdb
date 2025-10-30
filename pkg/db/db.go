package db

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mihn1/mdb/pkg/common"
	"github.com/mihn1/mdb/pkg/memtable"
	"github.com/mihn1/mdb/pkg/sstable"
	"github.com/mihn1/mdb/pkg/utils"
)

// DB represents the main database interface
type tableMeta struct {
	FileName   string
	Size       int64
	Level      int
	Seq        uint64
	CreatedAt  time.Time
	compacting bool
}

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

	compactionScheduled bool
	tablesMu            sync.RWMutex
	tables              []*tableMeta
	compactor           *Compactor
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
	db.compactor = NewCompactor(db.opts)
	if err := db.loadExistingTables(); err != nil {
		return nil, err
	}
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

	tablesSnapshot := db.snapshotTables()
	for _, meta := range tablesSnapshot {
		value, err := db.getFromTable(meta, key)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				break
			}
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
	meta, err := db.flushMemtable(imm)

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

	if meta == nil {
		return nil
	}
	if err := db.registerTable(meta); err != nil {
		return err
	}
	go db.maybeScheduleCompaction()
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

func (db *DB) flushMemtable(mem *memtable.MemTable) (*tableMeta, error) {
	// iterate over memtable
	// write to sstable file
	reader := mem.Reader()
	utils.AssertMsg(reader != nil, "memtable reader should not be nil")

	dir := db.getTablesDir()
	seq := atomic.AddUint64(&db.globalSeq, 1) - 1
	ts := time.Now().UnixNano()
	filePath := filepath.Join(dir, fmt.Sprintf("sstable-%d-%d-%d.sst", 0, seq, ts))
	tmpPath := filePath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	builder, err := sstable.NewTableBuilder(file, db.opts)
	if err != nil {
		file.Close()
		os.Remove(tmpPath)
		return nil, err
	}
	for ; reader.Valid(); reader.Next() {
		key, err := reader.Key()
		if err != nil {
			file.Close()
			os.Remove(tmpPath)
			return nil, err
		}
		value, err := reader.Value()
		if err != nil {
			file.Close()
			os.Remove(tmpPath)
			return nil, err
		}

		if err := builder.Add(key, value); err != nil {
			file.Close()
			os.Remove(tmpPath)
			return nil, err
		}
	}
	if err := builder.Finish(); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return nil, err
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	meta := &tableMeta{
		FileName:  filepath.Base(filePath),
		Size:      info.Size(),
		Level:     0,
		Seq:       seq,
		CreatedAt: time.Now(),
	}
	return meta, nil
}

func (db *DB) getTablesDir() string {
	dir := filepath.Join(db.path, "tables")
	utils.CreateDirIfNotExist(dir)
	return dir
}

func (db *DB) registerTable(meta *tableMeta) error {
	if meta == nil {
		return nil
	}
	if meta.Size == 0 {
		info, err := os.Stat(filepath.Join(db.getTablesDir(), meta.FileName))
		if err != nil {
			return err
		}
		meta.Size = info.Size()
	}
	db.tablesMu.Lock()
	db.addAndTidyTables(meta)
	db.tablesMu.Unlock()
	return nil
}

// Requires db.tablesMu to be held by caller
func (db *DB) addAndTidyTables(metas ...*tableMeta) {
	db.tables = append(db.tables, metas...)
	db.tidyTables()
}

// Requires db.tablesMu to be held by caller
func (db *DB) tidyTables() {
	sort.Slice(db.tables, func(i, j int) bool {
		if db.tables[i].Seq == db.tables[j].Seq {
			if db.tables[i].Level == db.tables[j].Level {
				return db.tables[i].CreatedAt.After(db.tables[j].CreatedAt)
			}
			return db.tables[i].Level < db.tables[j].Level
		}
		return db.tables[i].Seq > db.tables[j].Seq
	})
}

func (db *DB) snapshotTables() []*tableMeta {
	db.tablesMu.RLock()
	if len(db.tables) == 0 {
		db.tablesMu.RUnlock()
		return nil
	}
	snapshot := make([]*tableMeta, len(db.tables))
	copy(snapshot, db.tables)
	db.tablesMu.RUnlock()
	return snapshot
}

// Requires db.tablesMu to be held by caller
func (db *DB) markCompacting(metas []*tableMeta) {
	for _, meta := range metas {
		meta.compacting = true
	}
}

// Requires db.tablesMu to be held by caller
func (db *DB) releaseCompacting(metas []*tableMeta) {
	for _, meta := range metas {
		meta.compacting = false
	}
}

// Requires db.tablesMu to be held by caller
func (db *DB) removeTables(metas []*tableMeta) {
	if len(metas) == 0 {
		return
	}
	set := make(map[*tableMeta]struct{}, len(metas))
	for _, meta := range metas {
		meta.compacting = false
		set[meta] = struct{}{}
	}
	filtered := db.tables[:0]
	for _, meta := range db.tables {
		if _, found := set[meta]; found {
			continue
		}
		filtered = append(filtered, meta)
	}
	db.tables = filtered
}

func (db *DB) maybeScheduleCompaction() error {
	if db.compactor == nil {
		return nil
	}
	db.tablesMu.Lock()
	if db.compactionScheduled {
		db.tablesMu.Unlock()
		return nil
	}
	db.compactionScheduled = true
	db.tablesMu.Unlock()

	go func() {
		if err := db.compactor.Compact(db); err != nil {
			log.Printf("Compaction error: %v", err)
		}
		db.tablesMu.Lock()
		db.compactionScheduled = false
		db.tablesMu.Unlock()
	}()

	return nil
}

func (db *DB) loadExistingTables() error {
	dir := db.getTablesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	metas := make([]*tableMeta, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".sst" {
			continue
		}
		meta, err := parseTableMeta(dir, entry.Name())
		if err != nil {
			if db.opts.EnableDebugLogging {
				log.Printf("skipping table %s: %v", entry.Name(), err)
			}
			continue
		}
		metas = append(metas, meta)
	}
	if len(metas) == 0 {
		return nil
	}
	db.tablesMu.Lock()
	db.addAndTidyTables(metas...)
	db.tablesMu.Unlock()
	return nil
}

func parseTableMeta(dir, fileName string) (*tableMeta, error) {
	var level int
	var seq uint64
	var ts int64
	if _, err := fmt.Sscanf(fileName, "sstable-%d-%d-%d.sst", &level, &seq, &ts); err != nil {
		return nil, err
	}
	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil {
		return nil, err
	}
	return &tableMeta{
		FileName:  fileName,
		Size:      info.Size(),
		Level:     level,
		Seq:       seq,
		CreatedAt: info.ModTime(),
	}, nil
}

func (db *DB) getFromTable(meta *tableMeta, key []byte) ([]byte, error) {
	path := filepath.Join(db.getTablesDir(), meta.FileName)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	table, err := sstable.Open(file, db.opts)
	if err != nil {
		return nil, err
	}
	return table.Get(key)
}
