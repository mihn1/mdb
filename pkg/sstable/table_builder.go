package sstable

import (
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/mihn1/mdb/pkg/common"
	"github.com/mihn1/mdb/pkg/utils"
)

const (
	tableMagic   uint64 = 0x4D44535441424C45 // "MDSTABLE" (custom magic) - Simple checksum to ensure file integrity
	tableVersion uint32 = 1                  // Specify the table format version for readers (no really needed yet)
)

type blockMeta struct {
	size   uint32
	offset uint64
}

func (bm *blockMeta) encode() []byte {
	buf := make([]byte, 12)
	binary.LittleEndian.PutUint32(buf[:4], bm.size)
	binary.LittleEndian.PutUint64(buf[4:], bm.offset)
	return buf
}

// TableBuilder builds an SSTable in a single pass.
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
type TableBuilder struct {
	f                *os.File
	blockCount       int
	dataBlock        *blockBuilder
	indexBlock       *blockBuilder
	currentBlockMeta *blockMeta // Meta of the current data block to be added to index
	lastKey          []byte     // Last key added (for index entry)
	offset           uint64     // Current file offset
	closed           bool
	opts             *common.Options
}

func NewTableBuilder(file *os.File, opts *common.Options) (*TableBuilder, error) {
	return &TableBuilder{
		f:                file,
		blockCount:       0,
		dataBlock:        newBlockBuilder(opts),
		indexBlock:       newBlockBuilder(opts),
		currentBlockMeta: &blockMeta{},
		offset:           0,
		lastKey:          nil,
		closed:           false,
		opts:             opts,
	}, nil
}

// Add appends a key/value pair to the current block; keys must be provided in sorted order.
func (bb *TableBuilder) Add(key, value []byte) error {
	if bb.closed {
		return errors.New("writer closed")
	}

	// TODO: Implement adding to filter blocks

	bb.dataBlock.Add(key, value)
	bb.lastKey = key
	// Rough size estimate for the new record inside block buffer
	entrySize := bb.dataBlock.EstimateEntrySize(key, value)

	// If adding this record would exceed block size and current block not empty, flush block first
	if bb.dataBlock.CurrentEstimateSize()+entrySize+4 >= bb.opts.DataBlockSize { // +4 if new block will add entryCount header later
		if err := bb.flushDataBlock(); err != nil {
			return err
		}
	}
	return nil
}

// Write the current data block to file and reset the block builder
func (bb *TableBuilder) flushDataBlock() error {
	if bb.dataBlock.count == 0 {
		return nil // Nothing to flush
	}

	blockData := bb.dataBlock.Finish()
	// Create a new buffer: entryCount(4 bytes) + existing blockBuf.
	blockBytes := make([]byte, 0, 4+len(bb.dataBlock.buf))
	blockBytes = binary.LittleEndian.AppendUint32(blockBytes, uint32(bb.dataBlock.count))
	blockBytes = append(blockBytes, blockData...)
	// Write data block
	if _, err := bb.f.Write(blockBytes); err != nil {
		return err
	}

	// Update current block meta
	bb.currentBlockMeta.size = uint32(len(blockBytes))
	bb.currentBlockMeta.offset = bb.offset
	bb.blockCount++

	// Update file offset
	bb.offset += uint64(len(blockBytes))

	// Add to index block if we start a new data block
	bb.indexBlock.Add(bb.lastKey, bb.currentBlockMeta.encode())
	bb.dataBlock.Reset()
	return nil
}

// Finalize the table: flush pending block, write index block, footer, then closes file.
func (bb *TableBuilder) Finish() error {
	// Flush any remaining data block
	if err := bb.flushDataBlock(); err != nil {
		return err
	}
	utils.Assert(!bb.closed)

	// TODO: write filter block

	// Write index block
	indexData := bb.indexBlock.Finish()
	indexBytes := make([]byte, 0, 4+len(indexData))
	indexBytes = binary.LittleEndian.AppendUint32(indexBytes, uint32(bb.indexBlock.count))
	indexBytes = append(indexBytes, indexData...)
	indexOffset, err := bb.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := bb.f.Write(indexBytes); err != nil {
		return err
	}
	bb.offset += uint64(len(indexBytes))
	indexLength := uint64(len(indexBytes))

	// Write footerBytes
	footerBytes := make([]byte, 0, 28)
	footerBytes = binary.LittleEndian.AppendUint64(footerBytes, uint64(indexOffset))
	footerBytes = binary.LittleEndian.AppendUint64(footerBytes, indexLength)
	footerBytes = binary.LittleEndian.AppendUint32(footerBytes, tableVersion)
	footerBytes = binary.LittleEndian.AppendUint64(footerBytes, tableMagic)
	if _, err := bb.f.Write(footerBytes); err != nil {
		return err
	}
	bb.offset += uint64(len(footerBytes))

	// Close file
	if err := bb.f.Close(); err != nil {
		return err
	}
	bb.closed = true
	return nil
}
