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
