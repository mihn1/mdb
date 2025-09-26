package sstable

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/mihn1/mdb/pkg/memtable"
	"github.com/mihn1/mdb/pkg/utils"
)

/// table builder to create sstables from memtables

var globalSeq uint64

func BuildTable(mem *memtable.MemTable, path string) error {
	// iterate over memtable
	// write to sstable file
	// create index blocks
	// write footer
	// Write to a text file without any specific format for now
	iter := mem.Iterator()
	utils.AssertMsg(iter != nil, "memtable iterator should not be nil")

	dir := filepath.Join(path, "tables")
	utils.CreateDirIfNotExist(dir)
	seq := atomic.AddUint64(&globalSeq, 1) - 1
	ts := time.Now().UnixNano()
	filePath := filepath.Join(dir, fmt.Sprintf("sstable-%d-%d-%d.sst", 0, seq, ts))
	w, err := NewWriter(filePath)
	if err != nil {
		return err
	}
	for ; iter.Valid(); iter.Next() {
		if err := w.Add(iter.Key(), iter.Value()); err != nil {
			return err
		}
	}
	return w.Finish()
}
