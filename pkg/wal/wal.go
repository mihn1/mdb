package wal

import "io"

// WAL represents the write-ahead log
type WAL struct {
	// TODO: Add fields for log file, writer, etc.
}

// Entry represents a log entry
type Entry struct {
	Key   []byte
	Value []byte
	Type  EntryType
}

// TODO: maybe move to a separate file (types.go)?
// EntryType represents the type of log entry
type EntryType int

const (
	EntryTypePut EntryType = iota
	EntryTypeDelete
)

// Create a new WAL
func New(w io.Writer) *WAL {
	return &WAL{}
}

// Append an entry to the log
func (w *WAL) Append(entry *Entry) error {
	// TODO: Serialize and write entry
	return nil
}

// Force a sync to disk
func (w *WAL) Sync() error {
	// TODO: Implement fsync
	return nil
}

// Reader reads entries from a WAL file
type Reader struct {
	// TODO: Add fields for reading WAL
}

// NewReader creates a WAL reader
func NewReader(r io.Reader) *Reader {
	return &Reader{}
}

// Read the next entry
func (r *Reader) Next() (*Entry, error) {
	// TODO: Deserialize next entry
	return nil, nil
}
