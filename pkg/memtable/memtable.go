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
	value = append(value, byte(internal.TypeValue))
	return m.sl.Put(key, value)
}

func (m *MemTable) Get(key []byte) ([]byte, bool) {
	val, found := m.sl.Get(key)
	if !found {
		return nil, false
	}

	// Get the value type embedded in the last byte
	valType := val[len(val)-1]
	if valType == byte(internal.TypeTombstone) {
		return nil, false
	}

	return val[:len(val)-1], true
}

func (m *MemTable) Delete(key []byte) error {
	value := []byte{byte(internal.TypeTombstone)}
	return m.sl.Put(key, value)
}

// Return the approximate size in bytes
func (m *MemTable) Size() uint64 {
	return m.sl.Size()
}

// Return an iterator over the MemTable
func (m *MemTable) Iterator() internal.Iterator {
	return m.sl.Iterator()
}
