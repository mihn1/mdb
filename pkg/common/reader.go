package common

// Interface for traversing key-value pairs
type Reader interface {
	Valid() bool
	Key() ([]byte, error)
	Value() ([]byte, error)
	Next()
	Seek(key []byte)
	SeekToFirst()
}
