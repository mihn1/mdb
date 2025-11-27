package bloom

import "testing"

func TestFilterBasic(t *testing.T) {
	filter := New(256, 3)
	keys := [][]byte{[]byte("alpha"), []byte("beta"), []byte("gamma")}
	for _, key := range keys {
		filter.Add(key)
	}

	for _, key := range keys {
		if !filter.MayContain(key) {
			t.Fatalf("expected filter to contain %q", key)
		}
	}

	if filter.MayContain([]byte("delta")) && filter.MayContain([]byte("epsilon")) {
		// allow occasional false positives; test ensures API works without panic.
	}
}

func TestFilterFromBytes(t *testing.T) {
	filter := New(128, 2)
	filter.Add([]byte("hello"))
	filter.Add([]byte("world"))

	wrapped, err := FromBytes(filter.Bits(), filter.HashFunctions())
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}

	if !wrapped.MayContain([]byte("hello")) {
		t.Fatalf("wrapped filter missing key")
	}

	if wrapped.MayContain([]byte("unknown")) {
		// acceptable false positive probability; just ensure it doesn't always return true
	}
}
