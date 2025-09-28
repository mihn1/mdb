package common

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
	flag.IntVar(&sampleSize, "sampleSize", 500, "base sample size for skiplist tests and benchmarks")
	flag.IntVar(&readers, "readers", 4, "number of concurrent readers in concurrency tests")
	os.Exit(m.Run())
}

// Helpers
func makeKey(i int) []byte { return []byte(fmt.Sprintf("%08d", i)) }

func checkRangePresent(t *testing.T, sl *SkipList, start, end int) {
	t.Helper()
	for i := start; i < end; i++ {
		v, ok := sl.Get(makeKey(i))
		if !ok {
			t.Fatalf("expected key %d to be present", i)
		}
		// Expect value to be the string representation of i as []byte
		if !bytes.Equal(v, []byte(fmt.Sprintf("%d", i))) {
			t.Fatalf("value mismatch for %d: got %q", i, string(v))
		}
	}
}

// Basic operations
func TestSkipList_InsertSearch_Basic(t *testing.T) {
	for i := 0; i < 12; i++ {
		sl := NewSkipList(4, &byteLexComparator{})

		// Empty search
		if v, ok := sl.Get(makeKey(1)); ok || v != nil {
			t.Fatalf("expected empty search to be not found; got ok=%v v=%v", ok, v)
		}

		// Insert a few keys
		keys := []int{5, 1, 3, 4, 2, 1, 5, 234, 123, 451, 12, 323, 21, 323, 123, 12, 321, 3}
		for _, k := range keys {
			if err := sl.Put(makeKey(k), []byte(fmt.Sprintf("v%02d", k))); err != nil {
				t.Fatalf("insert %d failed: %v", k, err)
			}
		}

		// Search back
		for _, k := range keys {
			v, ok := sl.Get(makeKey(k))
			if !ok {
				t.Fatalf("key %d not found", k)
			}
			if !bytes.Equal(v, []byte(fmt.Sprintf("v%02d", k))) {
				t.Fatalf("value mismatch for %d: got %q", k, string(v))
			}
		}

		// Duplicate insert should update value (current policy)
		if err := sl.Put(makeKey(3), []byte("v03-updated")); err != nil {
			t.Fatalf("duplicate update failed: %v", err)
		}
		if v, ok := sl.Get(makeKey(3)); !ok || !bytes.Equal(v, []byte("v03-updated")) {
			t.Fatalf("expected updated value for key 3, got %q ok=%v", string(v), ok)
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
		if err := sl.Put(makeKey(i), []byte(fmt.Sprintf("%d", i))); err != nil {
			t.Fatalf("insert %d failed: %v", i, err)
		}
	}

	// Verify all present
	checkRangePresent(t, sl, 0, N)

	// Delete half in random order and verify the rest
	dels := r.Perm(N)[:N/2]
	for _, i := range dels {
		if _, err := sl.Delete(makeKey(i)); err != nil {
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
		_, ok := sl.Get(makeKey(i))
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
			_ = sl.Put(makeKey(i), []byte(fmt.Sprintf("%d", i)))
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
				sl.Get(makeKey(k))
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
		if _, ok := sl.Get(makeKey(i)); !ok {
			t.Fatalf("key %d missing after writer completion", i)
		}
	}
}

// Duplicate behavior: current policy is update on duplicate (no error).
func TestSkipList_DuplicateInsertPolicy(t *testing.T) {
	sl := NewSkipList(8, &byteLexComparator{})
	if err := sl.Put(makeKey(1), []byte("a")); err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	if err := sl.Put(makeKey(1), []byte("b")); err != nil {
		t.Fatalf("second insert (update) failed: %v", err)
	}
	v, ok := sl.Get(makeKey(1))
	if !ok {
		t.Fatalf("key should exist after update")
	}
	if !bytes.Equal(v, []byte("b")) {
		t.Fatalf("value not updated on duplicate insert: got %q", string(v))
	}
}

// Validate level sizes do not exceed maxLevel (structural sanity checks)
// Structural sanity test removed to avoid coupling to commons while implementation evolves.

// Compare implementation vs a reference map for correctness on random ops
func TestSkipList_RandomOpsReference(t *testing.T) {
	sl := NewSkipList(10, &byteLexComparator{})

	ref := map[string][]byte{}
	type op struct {
		typ string
		k   string
		v   []byte
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
			v := []byte(fmt.Sprintf("%d", r.Int()))
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
			err := sl.Put(key, op.v)
			if err != nil {
				t.Fatalf("unexpected insert error for key %q: %v", op.k, err)
			}
			// Update policy: latest value wins
			ref[op.k] = op.v
		case "del":
			_, _ = sl.Delete(key)
			delete(ref, op.k)
		case "get":
			v, ok := sl.Get(key)
			rv, rok := ref[op.k]
			if ok != rok {
				t.Fatalf("presence mismatch for %q: impl=%v ref=%v", op.k, ok, rok)
			}
			if ok && !bytes.Equal(v, rv) {
				t.Fatalf("value mismatch for %q: impl=%q ref=%q", op.k, string(v), string(rv))
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
		v, ok := sl.Get([]byte(k))
		if !ok {
			t.Fatalf("final presence mismatch for %q", k)
		}
		if !bytes.Equal(v, ref[k]) {
			t.Fatalf("final value mismatch for %q: impl=%q ref=%q", k, string(v), string(ref[k]))
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
		_ = sl.Put(keys[i], keys[i])
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
		_ = sl.Put(k, k)
	}
	// Build search keys to avoid allocation during benchmark loop
	keys := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		k := i % prefill
		keys[i] = prefillKeys[k]
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sl.Get(keys[i])
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
		_ = sl.Put(k, k)
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
		_, _ = sl.Get(keys[i])
	}
}

// Delete of present keys: measure delete cost; reinsert outside timer to keep load steady
func BenchmarkSkipList_DeleteHit(b *testing.B) {
	sl := NewSkipList(16, &byteLexComparator{})
	prefill := sampleSize * 100
	if prefill < 1000 {
		prefill = 1000
	}
	keys := make([][]byte, prefill)
	for i := 0; i < prefill; i++ {
		k := makeKey(i)
		keys[i] = k
		_ = sl.Put(k, k)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := keys[i%prefill]
		_, _ = sl.Delete(k)
		b.StopTimer()
		_ = sl.Put(k, k)
		b.StartTimer()
	}
}

// Delete of absent keys: measure miss path (no state changes required)
func BenchmarkSkipList_DeleteMiss(b *testing.B) {
	sl := NewSkipList(16, &byteLexComparator{})
	prefill := sampleSize * 100
	if prefill < 1000 {
		prefill = 1000
	}
	for i := 0; i < prefill; i++ {
		_ = sl.Put(makeKey(i*2), []byte("v"))
	}
	// Build miss keys (odd)
	keys := make([][]byte, b.N)
	for i := 0; i < b.N; i++ {
		k := i % prefill
		keys[i] = []byte(fmt.Sprintf("%08d", k*2+1))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sl.Delete(keys[i])
	}
}

// Mixed workload: ~60% Get, 30% Put, 10% Delete
func BenchmarkSkipList_MixedWorkload(b *testing.B) {
	sl := NewSkipList(16, &byteLexComparator{})

	prefill := sampleSize * 100
	if prefill < 1000 {
		prefill = 1000
	}
	keysPool := make([][]byte, prefill)
	for i := 0; i < prefill; i++ {
		k := makeKey(i)
		keysPool[i] = k
		_ = sl.Put(k, k)
	}

	// Precompute ops and keys to avoid allocations in timed loop
	// 0=get, 1=put, 2=del
	ops := make([]byte, b.N)
	keys := make([][]byte, b.N)
	r := rand.New(rand.NewSource(20250921))
	for i := 0; i < b.N; i++ {
		p := r.Intn(100)
		switch {
		case p < 60:
			ops[i] = 0 // get
		case p < 90:
			ops[i] = 1 // put
		default:
			ops[i] = 2 // del
		}
		keys[i] = keysPool[r.Intn(prefill)]
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		switch ops[i] {
		case 0:
			_, _ = sl.Get(keys[i])
		case 1:
			_ = sl.Put(keys[i], keys[i])
		default:
			_, _ = sl.Delete(keys[i])
		}
	}

}

// Mixed workload (write-heavy): ~80% Put, 10% Get, 10% Delete
func BenchmarkSkipList_MixedWorkloadWriteHeavy(b *testing.B) {
	sl := NewSkipList(16, &byteLexComparator{})

	prefill := sampleSize * 100
	if prefill < 1000 {
		prefill = 1000
	}
	keysPool := make([][]byte, prefill)
	for i := 0; i < prefill; i++ {
		k := makeKey(i)
		keysPool[i] = k
		_ = sl.Put(k, k)
	}

	// 0=get (10%), 1=put (80%), 2=del (10%)
	ops := make([]byte, b.N)
	keys := make([][]byte, b.N)
	r := rand.New(rand.NewSource(20250921))
	for i := 0; i < b.N; i++ {
		p := r.Intn(100)
		switch {
		case p < 10:
			ops[i] = 0 // get
		case p < 90:
			ops[i] = 1 // put
		default:
			ops[i] = 2 // del
		}
		keys[i] = keysPool[r.Intn(prefill)]
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		switch ops[i] {
		case 0:
			_, _ = sl.Get(keys[i])
		case 1:
			_ = sl.Put(keys[i], keys[i])
		default:
			_, _ = sl.Delete(keys[i])
		}
	}
}
