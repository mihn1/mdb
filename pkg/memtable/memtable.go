package memtable

import (
	"github.com/mihn1/mdb/pkg/datastructure"
	"github.com/mihn1/mdb/pkg/internal"
)

// MemTable represents the in-memory sorted structure
type MemTable struct {
	sl *datastructure.SkipList
}

// Create a new MemTable
func New() *MemTable {
	// TODO: Make maxLevel configurable
	maxLevel := 16
	return &MemTable{
		sl: datastructure.NewSkipList(maxLevel, &datastructure.ByteSliceComparator{}),
	}
}

func (m *MemTable) Put(key, value []byte) error {
	return m.sl.Put(key, value)
}

func (m *MemTable) Get(key []byte) ([]byte, bool) {
	return m.sl.Get(key)
}

func (m *MemTable) Delete(key []byte) (bool, error) {
	return m.sl.Delete(key) // Delete the key in SkipList for now
}

// Return the approximate size in bytes
func (m *MemTable) Size() uint64 {
	return m.sl.Size()
}

// Return an iterator over the MemTable
func (m *MemTable) Iterator() internal.Iterator {
	return m.sl.Iterator()
}
