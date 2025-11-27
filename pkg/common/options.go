package common

type Options struct {
	MaxSkipListLevel   int
	Comparator         Comparator
	MemTableSize       uint64 // Maximum size of MemTable before flushing to disk
	DataBlockSize      int    // Target size for data blocks in SSTables
	EnableDebugLogging bool   // Enable detailed logging for debugging
	EnableBloomFilter  bool   // Toggle SSTable bloom filters
	BloomFilterBits    int    // Bit count per data-block filter
	BloomFilterHashes  int    // Hash probes per key

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
		EnableBloomFilter:  true,
		BloomFilterBits:    2048,
		BloomFilterHashes:  3,

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
		EnableBloomFilter:  true,
		BloomFilterBits:    1024,
		BloomFilterHashes:  3,

		// Compaction options
		CompactionFanIn: 4,
	}
}
