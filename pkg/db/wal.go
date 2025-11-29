package db

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/mihn1/mdb/pkg/common"
)

var (
	errCorruptWAL = errors.New("db: corrupt wal entry")
)

const walRecordHeaderSize = 9

// WAL represents the write-ahead log. It is safe for concurrent use.
type WAL struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	opts   *common.Options
}

// WALEntry represents a log entry.
type WALEntry struct {
	Key   []byte
	Value []byte
	Type  EntryType
}

// EntryType represents the type of log entry.
type EntryType int

const (
	EntryTypePut EntryType = iota
	EntryTypeDelete
)

func openWAL(path string, opts *common.Options) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &WAL{
		file:   file,
		writer: bufio.NewWriter(file),
		opts:   opts,
	}, nil
}

// Append appends a single entry to the WAL.
func (w *WAL) Append(entry *WALEntry) error {
	if w == nil {
		return errors.New("db: wal not initialized")
	}
	if entry == nil {
		return errors.New("db: nil wal entry")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	encoded := encodeWALEntry(entry)
	if _, err := w.writer.Write(encoded); err != nil {
		return err
	}

	return nil
}

// Replace truncates the WAL and writes the provided entries as the new contents.
func (w *WAL) Replace(entries []*WALEntry) error {
	if w == nil {
		return errors.New("db: wal not initialized")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return err
	}
	if err := w.file.Truncate(0); err != nil {
		return err
	}
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return err
	}

	w.writer.Reset(w.file)
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		encoded := encodeWALEntry(entry)
		if _, err := w.writer.Write(encoded); err != nil {
			return err
		}
	}
	return w.writer.Flush()
}

func encodeWALEntry(entry *WALEntry) []byte {
	total := walRecordHeaderSize + len(entry.Key) + len(entry.Value)
	buf := make([]byte, total)
	buf[0] = byte(entry.Type)
	binary.LittleEndian.PutUint32(buf[1:5], uint32(len(entry.Key)))
	binary.LittleEndian.PutUint32(buf[5:9], uint32(len(entry.Value)))
	offset := walRecordHeaderSize
	copy(buf[offset:], entry.Key)
	copy(buf[offset+len(entry.Key):], entry.Value)
	return buf
}

// Sync flushes the WAL contents to stable storage.
func (w *WAL) Sync() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		return err
	}
	return w.file.Sync()
}

// Close flushes and closes the underlying WAL file.
func (w *WAL) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.writer.Flush(); err != nil {
		_ = w.file.Close()
		return err
	}
	return w.file.Close()
}

// WALReader reads entries from a WAL file sequentially.
type WALReader struct {
	r *bufio.Reader
}

// NewReader creates a WAL reader from the provided io.Reader.
func NewReader(r io.Reader) *WALReader {
	return &WALReader{r: bufio.NewReader(r)}
}

// Next returns the next Entry from the WAL or io.EOF when no more entries exist.
func (r *WALReader) Next() (*WALEntry, error) {
	if r == nil || r.r == nil {
		return nil, io.EOF
	}

	header := make([]byte, walRecordHeaderSize)
	n, err := io.ReadFull(r.r, header)
	if err != nil {
		if err == io.EOF && n == 0 {
			return nil, io.EOF
		}
		if err == io.ErrUnexpectedEOF {
			if n == 0 {
				return nil, io.EOF
			}
			return nil, errCorruptWAL
		}
		if err == io.EOF && n > 0 {
			return nil, errCorruptWAL
		}
		return nil, err
	}

	entryType := EntryType(header[0])
	keyLen := binary.LittleEndian.Uint32(header[1:5])
	valueLen := binary.LittleEndian.Uint32(header[5:9])

	key := make([]byte, keyLen)
	if keyLen > 0 {
		if _, err := io.ReadFull(r.r, key); err != nil {
			if err == io.ErrUnexpectedEOF {
				return nil, errCorruptWAL
			}
			return nil, err
		}
	}

	value := make([]byte, valueLen)
	if valueLen > 0 {
		if _, err := io.ReadFull(r.r, value); err != nil {
			if err == io.ErrUnexpectedEOF {
				return nil, errCorruptWAL
			}
			return nil, err
		}
	}

	return &WALEntry{Type: entryType, Key: key, Value: value}, nil
}
