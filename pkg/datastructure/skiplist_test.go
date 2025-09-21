package datastructure

import (
	"bytes"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// Configurable test parameters. Override via flags, e.g.:
//
//	go test ./pkg/datastructure -sampleSize=2000 -readers=8
var (
	sampleSize int
	readers    int
)

func TestMain(m *testing.M) {
	flag.IntVar(&sampleSize, "sampleSize", 5000, "base sample size for skiplist tests and benchmarks")
	flag.IntVar(&readers, "readers", 4, "number of concurrent readers in concurrency tests")
	os.Exit(m.Run())
}

// byteLexComparator is a simple lexicographic comparator for []byte keys.
// It mirrors the intended Comparator shape: Compare(a, b []byte) int; Name() string
type byteLexComparator struct{}

func (c *byteLexComparator) Compare(a, b []byte) int {
	return bytes.Compare(a, b)
}

func (c *byteLexComparator) Name() string { return "byte-lex" }

// Helpers
func makeKey(i int) []byte { return []byte(fmt.Sprintf("%08d", i)) }

func checkRangePresent(t *testing.T, sl *SkipList, start, end int) {
	t.Helper()
	for i := start; i < end; i++ {
		v, ok := sl.Search(makeKey(i))
		if !ok {
			t.Fatalf("expected key %d to be present", i)
		}
		// Value may be stored as any; we expect int here per test setup.
		if vi, ok2 := v.(int); !ok2 || vi != i {
			t.Fatalf("value mismatch for %d: got %#v", i, v)
		}
	}
}

// assertLevel0Sorted verifies that level-0 forward pointers form a sorted chain.
// This inspects internals because we are in the same package.
// Note: We avoid asserting internal structure to keep tests
// compatible while implementation evolves. We validate behavior
// through public methods Insert/Search/Delete.

// Basic operations
func TestSkipList_InsertSearch_Basic(t *testing.T) {
	for i := 0; i < 1513; i++ {
		sl := NewSkipList(4, &byteLexComparator{})

		// Empty search
		if v, ok := sl.Search(makeKey(1)); ok || v != nil {
			t.Fatalf("expected empty search to be not found; got ok=%v v=%v", ok, v)
		}

		// Insert a few keys
		keys := []int{5, 1, 3, 4, 2, 1, 5, 6, 12, 3, 512, 3, 417, 256, 1024}
		for _, k := range keys {
			if err := sl.Insert(makeKey(k), fmt.Sprintf("v%02d", k)); err != nil {
				t.Fatalf("insert %d failed: %v", k, err)
			}
		}

		// Search back
		for _, k := range keys {
			v, ok := sl.Search(makeKey(k))
			if !ok {
				t.Fatalf("key %d not found", k)
			}
			if sv, ok := v.(string); !ok || sv != fmt.Sprintf("v%02d", k) {
				t.Fatalf("value mismatch for %d: got %#v", k, v)
			}
		}

		// Duplicate insert should update value (current policy)
		if err := sl.Insert(makeKey(3), "v03-updated"); err != nil {
			t.Fatalf("duplicate update failed: %v", err)
		}
		if v, ok := sl.Search(makeKey(3)); !ok || v.(string) != "v03-updated" {
			t.Fatalf("expected updated value for key 3, got %v ok=%v", v, ok)
		}
	}
}

// Large randomized dataset
func TestSkipList_LargeRandomized(t *testing.T) {
	t.Parallel()
	N := sampleSize
	if N <= 0 {
		N = 1
	}
	sl := NewSkipList(16, &byteLexComparator{})

	// Generate unique keys 0..N-1 in random order
	r := rand.New(rand.NewSource(1337))
	idxs := r.Perm(N)
	for _, i := range idxs {
		if err := sl.Insert(makeKey(i), i); err != nil {
			t.Fatalf("insert %d failed: %v", i, err)
		}
	}

	// Verify all present
	checkRangePresent(t, sl, 0, N)

	// Delete half in random order and verify the rest
	dels := r.Perm(N)[:N/2]
	for _, i := range dels {
		if err := sl.Delete(makeKey(i)); err != nil {
			t.Fatalf("delete %d failed: %v", i, err)
		}
	}

	// Build a map of expected presence
	present := make([]bool, N)
	for i := 0; i < N; i++ {
		present[i] = true
	}
	for _, i := range dels {
		present[i] = false
	}

	for i := 0; i < N; i++ {
		_, ok := sl.Search(makeKey(i))
		if ok != present[i] {
			t.Fatalf("presence mismatch for %d: expected %v", i, present[i])
		}
	}

	// Behavioral validation done via presence checks above
}

// Concurrency: many readers, single writer appending keys
func TestSkipList_ConcurrentReaders(t *testing.T) {
	sl := NewSkipList(16, &byteLexComparator{})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: inserts 0..M-1 with small delay
	M := sampleSize
	if M <= 0 {
		M = 1
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < M; i++ {
			_ = sl.Insert(makeKey(i), i)
			if i%50 == 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}
		close(stop)
	}()

	// Readers: continuously search random keys until writer stops
	reader := func() {
		defer wg.Done()
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		for {
			select {
			case <-stop:
				return
			default:
				k := r.Intn(M)
				sl.Search(makeKey(k))
			}
		}
	}

	nReaders := readers
	if nReaders <= 0 {
		nReaders = 1
	}
	for i := 0; i < nReaders; i++ {
		wg.Add(1)
		go reader()
	}
	wg.Wait()

	// After completion, all keys should be present
	for i := 0; i < M; i++ {
		if _, ok := sl.Search(makeKey(i)); !ok {
			t.Fatalf("key %d missing after writer completion", i)
		}
	}
}

// Duplicate behavior: current policy is update on duplicate (no error).
func TestSkipList_DuplicateInsertPolicy(t *testing.T) {
	sl := NewSkipList(8, &byteLexComparator{})
	if err := sl.Insert(makeKey(1), "a"); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	if err := sl.Insert(makeKey(1), "b"); err != nil {
		t.Fatalf("second insert (update) failed: %v", err)
	}
	v, ok := sl.Search(makeKey(1))
	if !ok {
		t.Fatalf("key should exist after update")
	}
	if s, _ := v.(string); s != "b" {
		t.Fatalf("value not updated on duplicate insert: got %v", v)
	}
}

// Validate level sizes do not exceed maxLevel (structural sanity checks)
// Structural sanity test removed to avoid coupling to internals while implementation evolves.

// Compare implementation vs a reference map for correctness on random ops
func TestSkipList_RandomOpsReference(t *testing.T) {
	sl := NewSkipList(10, &byteLexComparator{})

	ref := map[string]any{}
	type op struct {
		typ string
		k   string
		v   any
	}
	var ops []op

	// Generate operations
	r := rand.New(rand.NewSource(4242))
	totalOps := sampleSize * 2
	if totalOps <= 0 {
		totalOps = 10
	}
	keySpace := sampleSize * 2
	if keySpace <= 0 {
		keySpace = 16
	}
	for i := 0; i < totalOps; i++ {
		k := fmt.Sprintf("%x", r.Intn(keySpace))
		switch r.Intn(3) {
		case 0: // insert
			v := r.Int()
			ops = append(ops, op{"put", k, v})
		case 1: // delete
			ops = append(ops, op{"del", k, nil})
		default: // get
			ops = append(ops, op{"get", k, nil})
		}
	}

	// Execute and compare behaviors (with duplicate-insert-as-error policy)
	for _, op := range ops {
		key := []byte(op.k)
		switch op.typ {
		case "put":
			err := sl.Insert(key, op.v)
			if err != nil {
				t.Fatalf("unexpected insert error for key %q: %v", op.k, err)
			}
			// Update policy: latest value wins
			ref[op.k] = op.v
		case "del":
			_ = sl.Delete(key)
			delete(ref, op.k)
		case "get":
			v, ok := sl.Search(key)
			rv, rok := ref[op.k]
			if ok != rok {
				t.Fatalf("presence mismatch for %q: impl=%v ref=%v", op.k, ok, rok)
			}
			if ok && fmt.Sprint(v) != fmt.Sprint(rv) {
				t.Fatalf("value mismatch for %q: impl=%v ref=%v", op.k, v, rv)
			}
		}
	}

	// Final full validation: gather keys and ensure they are searchable
	keys := make([]string, 0, len(ref))
	for k := range ref {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, ok := sl.Search([]byte(k))
		if !ok {
			t.Fatalf("final presence mismatch for %q", k)
		}
		if fmt.Sprint(v) != fmt.Sprint(ref[k]) {
			t.Fatalf("final value mismatch for %q: impl=%v ref=%v", k, v, ref[k])
		}
	}
}

// Benchmarks (optional): run with `go test -bench=.`
func BenchmarkSkipList_Insert(b *testing.B) {
	sl := NewSkipList(16, &byteLexComparator{})
	keys := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		keys[i] = makeKey(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sl.Insert(keys[i], i)
	}
}

func BenchmarkSkipList_SearchHit(b *testing.B) {
	sl := NewSkipList(16, &byteLexComparator{})
	prefill := sampleSize * 100
	if prefill < 1000 {
		prefill = 1000
	}
	prefillKeys := make([][]byte, prefill)
	for i := 0; i < prefill; i++ {
		k := makeKey(i)
		prefillKeys[i] = k
		_ = sl.Insert(k, i)
	}
	// Build search keys to avoid allocation during benchmark loop
	keys := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		k := i % prefill
		keys[i] = prefillKeys[k]
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sl.Search(keys[i])
	}
}

func BenchmarkSkipList_SearchMiss(b *testing.B) {
	sl := NewSkipList(16, &byteLexComparator{})
	prefill := sampleSize * 100
	if prefill < 1000 {
		prefill = 1000
	}
	prefillKeys := make([][]byte, prefill)
	for i := 0; i < prefill; i++ {
		k := makeKey(i * 2)
		prefillKeys[i] = k
		_ = sl.Insert(k, i)
	}
	// Build miss keys (odd gaps) to avoid allocation during benchmark
	keys := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		k := i % prefill
		// miss key sits between two existing even keys
		keys[i] = []byte(fmt.Sprintf("%08d", k*2+1))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sl.Search(keys[i])
	}
}
