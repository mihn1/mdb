package sstable

import (
	"fmt"
	"os"
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
	// Simple pattern: level 0, sequence, timestamp
	filePath := filepath.Join(dir, fmt.Sprintf("sstable-%d-%d-%d.sst", 0, seq, ts))
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()
		_, err := fmt.Fprintf(f, "%s:%s\n", key, value)
		if err != nil {
			return err
		}
	}

	return nil
}
