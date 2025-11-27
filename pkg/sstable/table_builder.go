package sstable

import (
	"errors"
	"io"
	"os"

	"github.com/mihn1/mdb/pkg/common"
	"github.com/mihn1/mdb/pkg/utils"
)

const (
	tableMagic   uint64 = 0x4D44535441424C45 // "MDSTABLE" (custom magic)
	tableVersion uint32 = 2                  // Bumped after introducing bloom filter metadata
)

// TableBuilder builds an SSTable in a single pass.
// Layout:
//
//	[DataBlock]* [FilterBlock?] [IndexBlock] [Footer]
//
// DataBlock:
//
//	uint32 entryCount
//	  repeated: uint32 keyLen | uint32 valueLen | key | value
//
// IndexBlock:
//
//	uint32 indexEntryCount
//	  repeated: uint32 lastKeyLen | uint32 blockMetaLen | lastKey | blockMeta
//			where blockMeta is:
//			uint64 offset | uint64 size
//
// FilterBlock (optional, serialized via filter_block.go) stores one bloom filter per data block.
//
// Footer (fixed 44 bytes):
//
//	uint64 filterOffset | uint64 filterLength | uint64 indexOffset | uint64 indexLength | uint32 version | uint64 magic
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
	filterBuilder    *filterBlockBuilder
	blockKeys        [][]byte
}

func NewTableBuilder(file *os.File, opts *common.Options) (*TableBuilder, error) {
	utils.AssertMsg(file != nil, "file must be provided")
	utils.AssertMsg(opts != nil, "options must be provided")

	var filterBuilder *filterBlockBuilder
	if opts.EnableBloomFilter {
		filterBuilder = newFilterBlockBuilder(opts.BloomFilterBits, opts.BloomFilterHashes)
	}

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
		filterBuilder:    filterBuilder,
		blockKeys:        make([][]byte, 0, 64),
	}, nil
}

// Add appends a key/value pair to the current block; keys must be provided in sorted order.
func (tb *TableBuilder) Add(key, value []byte) error {
	if tb.closed {
		return errors.New("writer closed")
	}

	entrySize := tb.dataBlock.EstimateEntrySize(key, value)
	// If adding this record would exceed block size and current block not empty, flush block first
	if tb.dataBlock.CurrentEstimateSize()+entrySize > tb.opts.DataBlockSize {
		if err := tb.flushDataBlock(); err != nil {
			return err
		}
	}

	tb.dataBlock.Add(key, value)
	tb.lastKey = key
	if tb.filterBuilder != nil {
		keyCopy := append([]byte(nil), key...)
		tb.blockKeys = append(tb.blockKeys, keyCopy)
	}
	return nil
}

// Write the current data block to file and reset the block builder
func (tb *TableBuilder) flushDataBlock() error {
	if tb.dataBlock.count == 0 {
		return nil // Nothing to flush
	}

	blockData := tb.dataBlock.Finish()
	// Create a new buffer: entryCount(4 bytes) + existing blockBuf.
	// Write data block
	if _, err := tb.f.Write(blockData); err != nil {
		return err
	}

	// Update current block meta
	tb.currentBlockMeta.size = uint64(len(blockData))
	tb.currentBlockMeta.offset = tb.offset
	tb.blockCount++

	// Update file offset
	tb.offset += uint64(len(blockData))
	// Add to index block if we start a new data block
	tb.indexBlock.Add(tb.lastKey, tb.currentBlockMeta.encode())
	tb.dataBlock.Reset()
	tb.flushFilterForBlock()
	return nil
}

func (tb *TableBuilder) flushFilterForBlock() {
	if tb.filterBuilder == nil {
		tb.blockKeys = tb.blockKeys[:0]
		return
	}
	tb.filterBuilder.AddBlock(tb.blockKeys)
	// Reuse backing array without reallocating
	for i := range tb.blockKeys {
		tb.blockKeys[i] = nil
	}
	tb.blockKeys = tb.blockKeys[:0]
}

// Finalize the table: flush pending block, write index block, footer, then closes file.
func (tb *TableBuilder) Finish() error {
	// Flush any remaining data block
	if err := tb.flushDataBlock(); err != nil {
		return err
	}
	utils.Assert(!tb.closed)

	if tb.blockCount == 0 {
		return ErrEmptyTable
	}

	var filterBlockMeta *blockMeta
	if tb.filterBuilder != nil {
		filterData := tb.filterBuilder.Finish()
		if len(filterData) > 0 {
			offset, err := tb.f.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}
			if _, err := tb.f.Write(filterData); err != nil {
				return err
			}
			tb.offset += uint64(len(filterData))
			filterBlockMeta = &blockMeta{
				offset: uint64(offset),
				size:   uint64(len(filterData)),
			}
		}
	}

	// Write index block
	indexBlockData := tb.indexBlock.Finish()
	indexOffset, err := tb.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err := tb.f.Write(indexBlockData); err != nil {
		return err
	}
	tb.offset += uint64(len(indexBlockData))
	indexLength := uint64(len(indexBlockData))
	indexBlockMeta := &blockMeta{
		offset: uint64(indexOffset),
		size:   indexLength,
	}

	// Write footerBytes
	foot := &footer{
		filterBlockMeta: filterBlockMeta,
		indexBlockMeta:  indexBlockMeta,
		version:         tableVersion,
		magicNumber:     tableMagic,
	}
	footerBytes := foot.encode()
	if _, err := tb.f.Write(footerBytes); err != nil {
		return err
	}
	tb.offset += uint64(len(footerBytes))

	// Close file
	if err := tb.f.Close(); err != nil {
		return err
	}
	tb.closed = true
	return nil
}
