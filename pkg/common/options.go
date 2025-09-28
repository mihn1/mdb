package common

type Options struct {
	MaxSkipListLevel int
	Comparator       Comparator
	MemTableSize     uint64 // Maximum size of MemTable before flushing to disk
	DataBlockSize    int    // Target size for data blocks in SSTables
}

func NewDefaultOptions() *Options {
	return &Options{
		MaxSkipListLevel: 12,
		MemTableSize:     4 * 1024 * 1024, // 4MB
		DataBlockSize:    4 * 1024,        // 4KB
		Comparator:       &ByteSliceComparator{},
	}
}
