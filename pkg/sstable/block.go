package sstable

import (
	"encoding/binary"

	"github.com/mihn1/mdb/pkg/common"
)

// Block represents a block of data in an SSTable.
type Block struct {
	data []byte
	opts *common.Options
}

func NewBlock(data []byte, opts *common.Options) *Block {
	return &Block{data: data, opts: opts}
}

type BlockReader struct {
	block      *Block
	current    int // Current read position
	entryIndex int
}

func (b *Block) NewReader() *BlockReader {
	return &BlockReader{block: b, current: 0, entryIndex: 0}
}

func (b *BlockReader) SeekToFirst() {
	b.current = 0
	b.entryIndex = 0
}

func (b *BlockReader) Seek(target []byte) {
	// Linear search all keys in the block for now
	b.current = 0
	b.entryIndex = 0
	for b.Valid() {
		keyLen := binary.LittleEndian.Uint32(b.block.data[b.current : b.current+4])
		key := b.block.data[b.current+8 : b.current+8+int(keyLen)]
		cmpResult := b.block.opts.Comparator.Compare(key, target)
		if cmpResult >= 0 {
			return
		}
		valLen := binary.LittleEndian.Uint32(b.block.data[b.current+4 : b.current+8])
		b.current += 8 + int(keyLen) + int(valLen)
		b.entryIndex++
	}
	// If we reach here, target is greater than all keys, so set to invalid position
	b.current = len(b.block.data)
}

func (b *BlockReader) Next() {
	if b.Valid() {
		keyLen := binary.LittleEndian.Uint32(b.block.data[b.current : b.current+4])
		valLen := binary.LittleEndian.Uint32(b.block.data[b.current+4 : b.current+8])
		b.current += 8 + int(keyLen) + int(valLen)
		b.entryIndex++
	}
}

func (b *BlockReader) Key() ([]byte, error) {
	if !b.Valid() {
		return nil, ErrInvalidBlockReader
	}
	keyLen := binary.LittleEndian.Uint32(b.block.data[b.current : b.current+4])
	keyStart := b.current + 8
	return b.block.data[keyStart : keyStart+int(keyLen)], nil
}

func (b *BlockReader) Value() ([]byte, error) {
	if !b.Valid() {
		return nil, ErrInvalidBlockReader
	}
	keyLen := binary.LittleEndian.Uint32(b.block.data[b.current : b.current+4])
	valLen := binary.LittleEndian.Uint32(b.block.data[b.current+4 : b.current+8])
	valStart := b.current + 8 + int(keyLen)
	return b.block.data[valStart : valStart+int(valLen)], nil
}

func (b *BlockReader) Valid() bool {
	return b.current < len(b.block.data)
}

// EntryIndex returns the ordinal position of the current entry within the block.
func (b *BlockReader) EntryIndex() int {
	return b.entryIndex
}
