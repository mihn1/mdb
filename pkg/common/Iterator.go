package common

// Interface for traversing key-value pairs
type Iterator interface {
	Valid() bool
	Key() ([]byte, error)
	Value() ([]byte, error)
	Next()
	Seek(key []byte)
	SeekToFirst()
}
