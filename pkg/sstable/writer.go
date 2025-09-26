package sstable

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
)

const (
	tableMagic       uint64 = 0x4D44535441424C45 // "MDSTABLE" (custom magic) - Simple checksum to ensure file integrity
	tableVersion     uint32 = 1                  // Specify the table format version for readers (no really needed yet)
	defaultBlockSize        = 8 * 1024           // 8KB target block size
)

// Writer builds an SSTable in a single pass.
// Layout:
//
//	[DataBlock]* [IndexBlock] [Footer]
//
// DataBlock:
//
//	uint32 entryCount
//	  repeated: uint32 keyLen | uint32 valueLen | key | value
//
// IndexBlock:
//
//	uint32 indexEntryCount
//	  repeated: uint32 lastKeyLen | lastKey | uint64 blockOffset | uint32 blockLength
//
// Footer (fixed 28 bytes):
//
//	uint64 indexOffset | uint64 indexLength | uint32 version | uint64 magic
//
// All integers little-endian.
type Writer struct {
	f          *os.File
	blockBuf   []byte
	blockCount int
	index      []indexEntry
	closed     bool
}

type indexEntry struct {
	lastKey     []byte
	blockOffset uint64
	blockLength uint32
}

// NewWriter creates a new SSTable writer targeting the given file path.
func NewWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	return &Writer{f: f, blockBuf: make([]byte, 0, defaultBlockSize)}, nil
}

// Add appends a key/value pair to the current block; keys must be provided in sorted order.
func (w *Writer) Add(key, value []byte) error {
	if w.closed {
		return errors.New("writer closed")
	}
	// Rough size estimate for the new record inside block buffer
	recSize := 4 + 4 + len(key) + len(value)
	// If adding this record would exceed block size and current block not empty, flush block first
	if len(w.blockBuf) > 0 && len(w.blockBuf)+recSize+4 > defaultBlockSize { // +4 if new block will add entryCount header later
		if err := w.flushBlock(); err != nil {
			return err
		}
	}
	// Append record: (keyLen, valueLen, key, value)
	w.blockBuf = appendUint32(w.blockBuf, uint32(len(key)))
	w.blockBuf = appendUint32(w.blockBuf, uint32(len(value)))
	w.blockBuf = append(w.blockBuf, key...)
	w.blockBuf = append(w.blockBuf, value...)
	w.blockCount++
	return nil
}

func (w *Writer) flushBlock() error {
	if w.blockCount == 0 {
		return nil
	}
	// Prepend entryCount at the beginning -> store entries sequentially without the header yet.
	// Create a new buffer: entryCount(4 bytes) + existing blockBuf.
	tmp := make([]byte, 0, 4+len(w.blockBuf))
	tmp = appendUint32(tmp, uint32(w.blockCount))
	tmp = append(tmp, w.blockBuf...)
	// Write block
	offset, err := w.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := w.f.Write(tmp); err != nil {
		return err
	}
	// Derive last key from last record in block (need to parse back). Simpler: parse from end.
	lastKey, err := extractLastKey(tmp)
	if err != nil {
		return err
	}
	w.index = append(w.index, indexEntry{lastKey: lastKey, blockOffset: uint64(offset), blockLength: uint32(len(tmp))})
	// Reset block buffer
	w.blockBuf = w.blockBuf[:0]
	w.blockCount = 0
	return nil
}

// Finish finalizes the table: flush pending block, write index block, footer, then closes file.
func (w *Writer) Finish() error {
	if w.closed {
		return nil
	}
	if err := w.flushBlock(); err != nil {
		return err
	}
	// Write index block
	indexOffset, err := w.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	ibuf := make([]byte, 0, 16*len(w.index))
	ibuf = appendUint32(ibuf, uint32(len(w.index)))
	for _, ie := range w.index {
		ibuf = appendUint32(ibuf, uint32(len(ie.lastKey)))
		ibuf = append(ibuf, ie.lastKey...)
		ibuf = appendUint64(ibuf, ie.blockOffset)
		ibuf = appendUint32(ibuf, ie.blockLength)
	}
	if _, err := w.f.Write(ibuf); err != nil {
		return err
	}
	indexLength := uint64(len(ibuf))
	// Footer
	footer := make([]byte, 0, 28)
	footer = appendUint64(footer, uint64(indexOffset))
	footer = appendUint64(footer, indexLength)
	footer = appendUint32(footer, tableVersion)
	footer = appendUint64(footer, tableMagic)
	if _, err := w.f.Write(footer); err != nil {
		return err
	}
	if err := w.f.Close(); err != nil {
		return err
	}
	w.closed = true
	return nil
}

// TODO: move below to utils package
// append helpers
func appendUint32(b []byte, v uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return append(b, buf[:]...)
}

func appendUint64(b []byte, v uint64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return append(b, buf[:]...)
}

// extractLastKey parses the last key from a block buffer that includes entryCount prefix.
func extractLastKey(block []byte) ([]byte, error) {
	if len(block) < 4 {
		return nil, errors.New("block too small")
	}
	entryCount := binary.LittleEndian.Uint32(block[:4])
	// Parse sequentially (simpler than reverse walking) because blocks are small (<=8KB).
	p := 4
	var lastKey []byte
	for i := uint32(0); i < entryCount; i++ {
		if p+8 > len(block) {
			return nil, errors.New("corrupt block record header")
		}
		klen := binary.LittleEndian.Uint32(block[p : p+4])
		vlen := binary.LittleEndian.Uint32(block[p+4 : p+8])
		p += 8
		if p+int(klen)+int(vlen) > len(block) {
			return nil, errors.New("corrupt block record body")
		}
		lastKey = block[p : p+int(klen)]
		p += int(klen) + int(vlen)
	}
	// copy lastKey because block slice will be reused (though we pass a tmp copy actually, still safe)
	out := make([]byte, len(lastKey))
	copy(out, lastKey)
	return out, nil
}
