package memtable

import (
	"github.com/mihn1/mdb/pkg/common"
)

// MemTable represents the in-memory sorted structure
type MemTable struct {
	sl *common.SkipList
}

// Create a new MemTable
func New(opts *common.Options) *MemTable {
	return &MemTable{
		sl: common.NewSkipList(opts.MaxSkipListLevel, opts.Comparator),
	}
}

func (m *MemTable) Put(key, value []byte) error {
	return m.sl.Put(key, value)
}

func (m *MemTable) Get(key []byte) ([]byte, bool) {
	val, found := m.sl.Get(key)
	if !found {
		return nil, false
	}

	return val, true
}

// Return the approximate size in bytes
func (m *MemTable) Size() uint64 {
	return m.sl.Size()
}

// Return an iterator over the MemTable
func (m *MemTable) Iterator() common.Reader {
	return m.sl.Iterator()
}
