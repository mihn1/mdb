package common

type Options struct {
	MaxSkipListLevel   int
	Comparator         Comparator
	MemTableSize       uint64 // Maximum size of MemTable before flushing to disk
	DataBlockSize      int    // Target size for data blocks in SSTables
	EnableDebugLogging bool   // Enable detailed logging for debugging

	// Compaction options
	CompactionFanIn int // Number of files to compact together per size tier
}

func NewDefaultOptions() *Options {
	return &Options{
		MaxSkipListLevel:   12,
		MemTableSize:       4 * 1024 * 1024, // 4MB
		DataBlockSize:      4 * 1024,        // 4KB
		Comparator:         &ByteSliceComparator{},
		EnableDebugLogging: false,

		// Compaction options
		CompactionFanIn: 4,
	}
}

func NewDebugOptions() *Options {
	return &Options{
		MaxSkipListLevel:   12,
		MemTableSize:       32 * 1024, // 32KB
		DataBlockSize:      4 * 1024,  // 4KB
		Comparator:         &ByteSliceComparator{},
		EnableDebugLogging: true,

		// Compaction options
		CompactionFanIn: 4,
	}
}
