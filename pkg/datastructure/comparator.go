package datastructure

type Comparator interface {
	Compare(a []byte, b []byte) int
	Name() string
}

type StringComparator struct{}

func (c *StringComparator) Compare(a []byte, b []byte) int {
	if a == nil && b == nil {
		return 0
	} else if a == nil {
		return -1
	} else if b == nil {
		return 1
	}

	if string(a) < string(b) {
		return -1
	} else if string(a) > string(b) {
		return 1
	}
	return 0
}

func (c *StringComparator) Name() string {
	return "StringComparator"
}

type ByteSliceComparator struct{}

func (c *ByteSliceComparator) Compare(a []byte, b []byte) int {
	minLen := min(len(b), len(a))

	for i := 0; i < minLen; i++ {
		if a[i] < b[i] {
			return -1
		} else if a[i] > b[i] {
			return 1
		}
	}

	if len(a) < len(b) {
		return -1
	} else if len(a) > len(b) {
		return 1
	}
	return 0
}

func (c *ByteSliceComparator) Name() string {
	return "ByteSliceComparator"
}
