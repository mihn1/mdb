package sstable

import (
	"io"

	"github.com/mihn1/mdb/pkg/internal"
)

// SSTable represents an immutable sorted string table
type SSTable struct {
	// TODO: Add fields for file handle, index, etc.
}

// Reader reads from existing SSTables
type Reader struct {
	// TODO: Add fields for reading SSTable
}

// Create a new SSTable reader
func NewReader(r io.Reader) (*Reader, error) {
	// TODO: Parse SSTable format and build index
	return nil, nil
}

// Lookup a key in the SSTable
func (r *Reader) Get(key []byte) ([]byte, bool, error) {
	// TODO: Binary search in index, then read block
	return nil, false, nil
}

// Return an iterator over the SSTable
func (r *Reader) Iterator() internal.Iterator {
	// TODO: Implement SSTable iterator
	return nil
}
