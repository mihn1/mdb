package bloom

// Filter represents a Bloom filter
type Filter struct {
	// TODO: Add bit array and hash functions
}

// New creates a new Bloom filter
func New(expectedElements int, falsePositiveRate float64) *Filter {
	return &Filter{}
}

func (f *Filter) Add(key []byte) {
	// TODO: Hash key and set bits
}

func (f *Filter) MayContain(key []byte) bool {
	// TODO: Hash key and check bits
	return false
}
