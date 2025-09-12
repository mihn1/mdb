package sstable

import "io"

// SSTable represents an immutable sorted string table
type SSTable struct {
	// TODO: Add fields for file handle, index, etc.
}

// Writer builds SSTables from sorted data
type Writer struct {
	// TODO: Add fields for building SSTable
}

// Reader reads from existing SSTables
type Reader struct {
	// TODO: Add fields for reading SSTable
}

// Create a new SSTable writer
func NewWriter(w io.Writer) *Writer {
	return &Writer{}
}

// Add a key-value pair (keys must be added in sorted order)
func (w *Writer) Add(key, value []byte) error {
	// TODO: Implement SSTable building
	return nil
}

// Finish building the SSTable and writes metadata
func (w *Writer) Finish() error {
	// TODO: Write index blocks and footer
	return nil
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
func (r *Reader) Iterator() Iterator {
	// TODO: Implement SSTable iterator
	return nil
}

// Iterator interface for traversing SSTable
type Iterator interface {
	Valid() bool
	Key() []byte
	Value() []byte
	Next()
	Seek(key []byte)
}
