package sstable

import (
	"encoding/binary"

	"github.com/mihn1/mdb/pkg/bloom"
)

// filterBlockBuilder constructs the serialized bloom filter block for an SSTable.
// Encoding (little endian):
//
//	uint32 filterCount   - number of data blocks / filters
//	uint32 filterBytes   - size of each filter in bytes (fixed for all blocks)
//	uint32 hashFuncs     - number of hash probes per key
//	repeated [filterBytes] byte arrays, one per data block in creation order
//
// Each individual filter is a fixed-size bit array built by github.com/mihn1/mdb/pkg/bloom.
type filterBlockBuilder struct {
	filterBytes int
	hashFuncs   int
	count       int
	buf         []byte
}

func newFilterBlockBuilder(filterBits int, hashFuncs int) *filterBlockBuilder {
	if filterBits <= 0 {
		filterBits = 2048
	}
	if filterBits%8 != 0 {
		filterBits += 8 - (filterBits % 8)
	}
	if hashFuncs <= 0 {
		hashFuncs = 3
	}
	return &filterBlockBuilder{
		filterBytes: filterBits / 8,
		hashFuncs:   hashFuncs,
		buf:         make([]byte, 0, filterBits/8*4),
	}
}

func (b *filterBlockBuilder) AddBlock(keys [][]byte) {
	filter := bloom.New(b.filterBytes*8, b.hashFuncs)
	for _, key := range keys {
		filter.Add(key)
	}
	b.buf = append(b.buf, filter.Bits()...)
	b.count++
}

func (b *filterBlockBuilder) Finish() []byte {
	header := make([]byte, 0, 12+len(b.buf))
	header = binary.LittleEndian.AppendUint32(header, uint32(b.count))
	header = binary.LittleEndian.AppendUint32(header, uint32(b.filterBytes))
	header = binary.LittleEndian.AppendUint32(header, uint32(b.hashFuncs))
	return append(header, b.buf...)
}

// filterBlockReader inspects serialized bloom filters stored in table files.
type filterBlockReader struct {
	filters     []byte
	filterCount int
	filterBytes int
	hashFuncs   int
}

func newFilterBlockReader(data []byte) (*filterBlockReader, error) {
	if len(data) < 12 {
		return nil, ErrInvalidFilterBlock
	}
	count := int(binary.LittleEndian.Uint32(data[:4]))
	filterBytes := int(binary.LittleEndian.Uint32(data[4:8]))
	hashFuncs := int(binary.LittleEndian.Uint32(data[8:12]))
	if count < 0 || filterBytes <= 0 || hashFuncs <= 0 {
		return nil, ErrInvalidFilterBlock
	}
	expected := 12 + count*filterBytes
	if len(data) != expected {
		return nil, ErrInvalidFilterBlock
	}
	return &filterBlockReader{
		filters:     data[12:],
		filterCount: count,
		filterBytes: filterBytes,
		hashFuncs:   hashFuncs,
	}, nil
}

func (r *filterBlockReader) mayContain(blockIndex int, key []byte) bool {
	if r == nil || blockIndex < 0 || blockIndex >= r.filterCount {
		return true
	}
	start := blockIndex * r.filterBytes
	bits := r.filters[start : start+r.filterBytes]
	filter, err := bloom.FromBytes(bits, r.hashFuncs)
	if err != nil {
		return true
	}
	return filter.MayContain(key)
}
