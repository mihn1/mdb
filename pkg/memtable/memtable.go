package memtable

// MemTable represents the in-memory sorted structure
type MemTable struct {
	// TODO: Implement skip list or balanced tree
}

// Create a new MemTable
func New() *MemTable {
	return &MemTable{}
}

func (m *MemTable) Put(key, value []byte) error {
	// TODO: Implement skip list insertion
	return nil
}

func (m *MemTable) Get(key []byte) ([]byte, bool) {
	// TODO: Implement skip list lookup
	return nil, false
}

func (m *MemTable) Delete(key []byte) error {
	// TODO: Implement tombstone insertion
	return nil
}

// Return the approximate size in bytes
func (m *MemTable) Size() int {
	// TODO: Calculate memory usage
	return 0
}

// Return an iterator over the MemTable
func (m *MemTable) Iterator() Iterator {
	// TODO: Implement iterator
	return nil
}

// Interface for traversing key-value pairs
type Iterator interface {
	Valid() bool
	Key() []byte
	Value() []byte
	Next()
	Seek(key []byte)
}
