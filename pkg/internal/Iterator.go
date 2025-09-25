package internal

// Interface for traversing key-value pairs
type Iterator interface {
	Valid() bool
	Key() []byte
	Value() []byte
	Next()
	Seek(key []byte)
	SeekToFirst()
}
