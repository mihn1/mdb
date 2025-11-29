package bloom

import "errors"

const (
	defaultBitsPerFilter = 2048
	defaultHashFuncs     = 3
	bloomSeed            = uint32(0xbc8f1d14)
)

// Filter is a simple Bloom filter backed by a fixed-size bit array.
type Filter struct {
	bits []byte
	k    uint32
}

// New creates a Bloom filter with the provided bitCount and number of hash functions.
// The provided bitCount will be rounded up to the next multiple of 8 bits.
func New(bitCount int, hashFuncs int) *Filter {
	if bitCount <= 0 {
		bitCount = defaultBitsPerFilter
	}
	if bitCount%8 != 0 {
		bitCount += 8 - (bitCount % 8)
	}
	if hashFuncs <= 0 {
		hashFuncs = defaultHashFuncs
	}
	if hashFuncs > 30 {
		hashFuncs = 30
	}
	return &Filter{
		bits: make([]byte, bitCount/8),
		k:    uint32(hashFuncs),
	}
}

// FromBytes wraps an existing bloom bit array and treats it as a filter with the
// provided hash function count. The returned filter shares the provided slice.
func FromBytes(bits []byte, hashFuncs int) (*Filter, error) {
	if len(bits) == 0 {
		return nil, errors.New("bloom: empty bit array")
	}
	if hashFuncs <= 0 {
		return nil, errors.New("bloom: hashFuncs must be > 0")
	}
	if hashFuncs > 30 {
		hashFuncs = 30
	}
	return &Filter{bits: bits, k: uint32(hashFuncs)}, nil
}

// Bits returns the raw bitset backing this filter.
func (f *Filter) Bits() []byte {
	return f.bits
}

// HashFunctions returns the number of hash probes configured for this filter.
func (f *Filter) HashFunctions() int {
	return int(f.k)
}

// Add inserts the provided key into the filter.
func (f *Filter) Add(key []byte) {
	if len(f.bits) == 0 || f.k == 0 {
		return
	}
	bits := uint32(len(f.bits) * 8)
	h := hash32(key)
	delta := rotate32(h)
	for i := uint32(0); i < f.k; i++ {
		bitpos := h % bits
		byteIdx := bitpos / 8
		mask := byte(1 << (bitpos % 8))
		f.bits[byteIdx] |= mask
		h += delta
	}
}

// MayContain reports whether the key might have been added to this filter.
// False means the key was definitely not inserted; true may be a false positive.
func (f *Filter) MayContain(key []byte) bool {
	if len(f.bits) == 0 || f.k == 0 {
		return false
	}
	bits := uint32(len(f.bits) * 8)
	h := hash32(key)
	delta := rotate32(h)
	for i := uint32(0); i < f.k; i++ {
		bitpos := h % bits
		byteIdx := bitpos / 8
		mask := byte(1 << (bitpos % 8))
		if f.bits[byteIdx]&mask == 0 {
			return false
		}
		h += delta
	}
	return true
}

func hash32(key []byte) uint32 {
	const prime uint32 = 16777619
	var hash uint32 = 2166136261 ^ bloomSeed
	for _, b := range key {
		hash ^= uint32(b)
		hash *= prime
	}
	return hash
}

func rotate32(v uint32) uint32 {
	return (v >> 17) | (v << 15)
}
