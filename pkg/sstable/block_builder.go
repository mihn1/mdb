package sstable

import (
	"encoding/binary"

	"github.com/mihn1/mdb/pkg/common"
	"github.com/mihn1/mdb/pkg/utils"
)

type blockBuilder struct {
	buf      []byte
	count    int
	opts     *common.Options
	lastKey  []byte
	finished bool
}

func newBlockBuilder(opts *common.Options) *blockBuilder {
	return &blockBuilder{
		buf:  make([]byte, 0, 4096), // Start with 4KB capacity
		opts: opts,
	}
}

func (bb *blockBuilder) CurrentEstimateSize() int {
	return len(bb.buf)
}

func (bb *blockBuilder) EstimateEntrySize(key []byte, val []byte) int {
	return 4 + 4 + len(key) + len(val)
}

func (bb *blockBuilder) Add(key []byte, val []byte) {
	utils.Assert(!bb.finished)
	utils.Assert(bb.count == 0 || bb.opts.Comparator.Compare(key, bb.lastKey) > 0)

	bb.buf = binary.LittleEndian.AppendUint32(bb.buf, uint32(len(key)))
	bb.buf = binary.LittleEndian.AppendUint32(bb.buf, uint32(len(val)))
	bb.buf = append(bb.buf, key...)
	bb.buf = append(bb.buf, val...)
	bb.count++
	bb.lastKey = key
}

func (bb *blockBuilder) Finish() []byte {
	bb.finished = true
	return bb.buf
}

func (bb *blockBuilder) Reset() {
	bb.buf = bb.buf[:0]
	bb.count = 0
	bb.lastKey = nil
	bb.finished = false
}

func (bb *blockBuilder) Empty() bool {
	return len(bb.buf) == 0
}
