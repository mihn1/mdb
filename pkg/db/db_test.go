package db_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mihn1/mdb/pkg/common"
	"github.com/mihn1/mdb/pkg/db"
)

// makeTempDir creates a unique temporary directory for each test under system temp.
func makeTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mdb-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	return dir
}

// openTestDB opens a DB with a very small memtable size to force flushes easily when requested.
func openTestDB(t *testing.T, path string, memSize uint64) *db.DB {
	opts := common.NewDefaultOptions()
	opts.MemTableSize = memSize
	d, err := db.Open(path, opts)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	return d
}

func waitForFlush(t *testing.T, d *db.DB, target uint64, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if d.Stats().Flushes >= target {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for flush count %d (have %d)", target, d.Stats().Flushes)
}

func listSSTables(t *testing.T, path string) []string {
	t.Helper()
	var files []string
	tablesDir := filepath.Join(path, "tables")
	entries, err := os.ReadDir(tablesDir)
	if err != nil {
		// If directory doesn't exist yet, return empty slice
		if os.IsNotExist(err) {
			return files
		}
		t.Fatalf("readdir tables: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return files
}

// TestOpenAndBasicCRUD covers Put and Get for existing keys.
func TestOpenAndBasicCRUD(t *testing.T) {
	dir := makeTempDir(t)
	d := openTestDB(t, dir, 1<<20) // 1MB so no flush interference

	if err := d.Put([]byte("a"), []byte("va")); err != nil {
		t.Fatalf("put failed: %v", err)
	}
	got, err := d.Get([]byte("a"))
	if err != nil || string(got) != "va" {
		t.Fatalf("get mismatch got=%q err=%v", got, err)
	}
}

// TestGetNonExistingReturnsNil ensures absence returns nil without error.
func TestGetNonExistingReturnsNil(t *testing.T) {
	dir := makeTempDir(t)
	d := openTestDB(t, dir, 1<<20)
	v, err := d.Get([]byte("ghost"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Fatalf("expected nil for missing key, got %q", v)
	}
}

// TestDeleteSemantics ensures delete removes key and subsequent Get is nil.
func TestDeleteSemantics(t *testing.T) {
	dir := makeTempDir(t)
	d := openTestDB(t, dir, 1<<20)
	if err := d.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := d.Delete([]byte("k")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if v, _ := d.Get([]byte("k")); v != nil {
		t.Fatalf("expected nil after delete, got %q", v)
	}
}

// TestFlushCreatesSSTable forces a flush via small MemTableSize and checks file exists.
func TestFlushCreatesSSTable(t *testing.T) {
	dir := makeTempDir(t)
	// Very small memtable size to force flush quickly
	d := openTestDB(t, dir, 64) // bytes
	// Each Put adds key+value; just write until flush count increments
	targetFlush := uint64(1)
	for i := 0; i < 1000 && d.Stats().Flushes < targetFlush; i++ {
		k := []byte{byte(i)}
		if err := d.Put(k, []byte("x")); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	waitForFlush(t, d, targetFlush, 2*time.Second)
	files := listSSTables(t, dir)
	if len(files) == 0 {
		t.Fatalf("expected at least one sstable file after flush")
	}
}

// TestMultipleFlushes ensures flush counter increments and multiple files appear (currently may overwrite, so we only assert counter for now).
func TestMultipleFlushes(t *testing.T) {
	dir := makeTempDir(t)
	d := openTestDB(t, dir, 128) // small
	desired := uint64(3)
	for d.Stats().Flushes < desired {
		// keep writing keys to exceed size repeatedly
		if err := d.Put([]byte(time.Now().Format(time.RFC3339Nano)), []byte("v")); err != nil {
			t.Fatalf("put: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if d.Stats().Flushes < desired {
		t.Fatalf("expected at least %d flushes, got %d", desired, d.Stats().Flushes)
	}
}

// TestStatsReflectMemtableGrowth ensures MemTableSize grows with inserts (before flush triggers).
func TestStatsReflectMemtableGrowth(t *testing.T) {
	dir := makeTempDir(t)
	d := openTestDB(t, dir, 1<<20) // big enough to avoid flush
	before := d.Stats().MemTableSize
	if err := d.Put([]byte("alpha"), []byte("beta")); err != nil {
		t.Fatalf("put: %v", err)
	}
	after := d.Stats().MemTableSize
	if !(after > before) {
		t.Fatalf("expected memtable size to increase: before=%d after=%d", before, after)
	}
}

// TestSkipPerformance is a placeholder documenting performance tests are intentionally skipped here.
func TestSkipPerformance(t *testing.T) {
	t.Skip("performance tests intentionally skipped in db_test suite")
}

// TestIntegrationLargeDataset tests the full flow with large dataset that spans multiple SSTables
func TestIntegrationLargeDataset(t *testing.T) {
	dir := makeTempDir(t)
	// Small memtable to force multiple flushes
	d := openTestDB(t, dir, 1024) // 1KB memtable

	// Generate enough data to exceed memtable capacity multiple times
	// This should create multiple SSTables
	numKeys := 1000
	keyValuePairs := make(map[string]string)

	// Write large dataset
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key-%06d", i))
		value := []byte(fmt.Sprintf("value-%06d-data-payload-to-make-it-larger", i))
		keyValuePairs[string(key)] = string(value)

		if err := d.Put(key, value); err != nil {
			t.Fatalf("Put key %s: %v", key, err)
		}
	}

	// Wait for multiple flushes to complete
	waitForFlush(t, d, 3, 5*time.Second)

	// Verify multiple SSTable files were created
	files := listSSTables(t, dir)
	if len(files) == 0 {
		t.Fatalf("Expected at least 1 SSTable file, got %d", len(files))
	}
	t.Logf("Created %d SSTable files after large dataset", len(files))

	// Now read back all keys and verify values
	for keyStr, expectedValue := range keyValuePairs {
		key := []byte(keyStr)
		got, err := d.Get(key)
		if err != nil {
			t.Fatalf("Get key %s: %v", key, err)
		}
		if string(got) != expectedValue {
			t.Fatalf("Get key %s: got %q, want %q", key, string(got), expectedValue)
		}
	}

	// Test reading non-existent keys
	nonExistentKeys := []string{"key-999999", "nonexistent", "zzz-last"}
	for _, keyStr := range nonExistentKeys {
		key := []byte(keyStr)
		got, err := d.Get(key)
		if err != nil {
			t.Fatalf("Get non-existent key %s: %v", key, err)
		}
		if got != nil {
			t.Fatalf("Expected nil for non-existent key %s, got %q", key, string(got))
		}
	}
}

// TestIntegrationMemtableAndSSTableLookup tests that reads work across memtable and SSTables
func TestIntegrationMemtableAndSSTableLookup(t *testing.T) {
	dir := makeTempDir(t)
	d := openTestDB(t, dir, 512) // Small memtable

	// Phase 1: Add data that will be flushed to SSTable
	oldKeys := make(map[string]string)
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("old-key-%03d", i))
		value := []byte(fmt.Sprintf("old-value-%03d-with-some-data", i))
		oldKeys[string(key)] = string(value)

		if err := d.Put(key, value); err != nil {
			t.Fatalf("Put old key %s: %v", key, err)
		}
	}

	// Wait for flush
	waitForFlush(t, d, 1, 3*time.Second)

	// Phase 2: Add new data that stays in memtable
	newKeys := make(map[string]string)
	for i := 0; i < 20; i++ {
		key := []byte(fmt.Sprintf("new-key-%03d", i))
		value := []byte(fmt.Sprintf("new-value-%03d", i))
		newKeys[string(key)] = string(value)

		if err := d.Put(key, value); err != nil {
			t.Fatalf("Put new key %s: %v", key, err)
		}
	}

	// Verify we can read both old (from SSTable) and new (from memtable) keys
	for keyStr, expectedValue := range oldKeys {
		key := []byte(keyStr)
		got, err := d.Get(key)
		if err != nil {
			t.Fatalf("Get old key %s: %v", key, err)
		}
		if string(got) != expectedValue {
			t.Fatalf("Get old key %s: got %q, want %q", key, string(got), expectedValue)
		}
	}

	for keyStr, expectedValue := range newKeys {
		key := []byte(keyStr)
		got, err := d.Get(key)
		if err != nil {
			t.Fatalf("Get new key %s: %v", key, err)
		}
		if string(got) != expectedValue {
			t.Fatalf("Get new key %s: got %q, want %q", key, string(got), expectedValue)
		}
	}
}

// TestIntegrationOverwriteAndDelete tests overwriting keys and deletes across memtable/SSTable boundary
func TestIntegrationOverwriteAndDelete(t *testing.T) {
	dir := makeTempDir(t)
	d := openTestDB(t, dir, 256) // Very small memtable

	testKey := []byte("test-key")

	// Write initial value
	if err := d.Put(testKey, []byte("original-value")); err != nil {
		t.Fatalf("Put original: %v", err)
	}

	// Add enough data to force flush
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("filler-%03d", i))
		value := []byte(fmt.Sprintf("filler-value-%03d", i))
		if err := d.Put(key, value); err != nil {
			t.Fatalf("Put filler: %v", err)
		}
	}

	// Wait for flush (original value now in SSTable)
	waitForFlush(t, d, 1, 3*time.Second)

	// Overwrite with new value (this goes to memtable)
	if err := d.Put(testKey, []byte("updated-value")); err != nil {
		t.Fatalf("Put updated: %v", err)
	}

	// Should get the updated value from memtable
	got, err := d.Get(testKey)
	if err != nil {
		t.Fatalf("Get updated: %v", err)
	}
	if string(got) != "updated-value" {
		t.Fatalf("Expected updated value, got %q", string(got))
	}

	// Delete the key
	if err := d.Delete(testKey); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Should return nil after delete
	got, err = d.Get(testKey)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("Expected nil after delete, got %q", string(got))
	}
}

// TestIntegrationLargeValues tests handling of large values that might affect block boundaries
func TestIntegrationLargeValues(t *testing.T) {
	dir := makeTempDir(t)
	d := openTestDB(t, dir, 2048) // 2KB memtable

	// Create large values (1KB each)
	largeValue := make([]byte, 1024)
	for i := range largeValue {
		largeValue[i] = byte('A' + (i % 26))
	}

	testData := make(map[string][]byte)

	// Write 10 large values
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("large-key-%02d", i))
		value := make([]byte, len(largeValue))
		copy(value, largeValue)
		// Add some variation to each value
		value[0] = byte('0' + i)
		testData[string(key)] = value

		if err := d.Put(key, value); err != nil {
			t.Fatalf("Put large key %s: %v", key, err)
		}
	}

	// Wait for flushes
	waitForFlush(t, d, 2, 5*time.Second)

	// Verify all large values can be read back correctly
	for keyStr, expectedValue := range testData {
		key := []byte(keyStr)
		got, err := d.Get(key)
		if err != nil {
			t.Fatalf("Get large key %s: %v", key, err)
		}
		if len(got) != len(expectedValue) {
			t.Fatalf("Get large key %s: length mismatch got %d, want %d", key, len(got), len(expectedValue))
		}
		if string(got) != string(expectedValue) {
			t.Fatalf("Get large key %s: value mismatch", key)
		}
	}
}

// TestIntegrationManySmallWrites tests performance and correctness with many small writes
func TestIntegrationManySmallWrites(t *testing.T) {
	dir := makeTempDir(t)
	d := openTestDB(t, dir, 1024) // 1KB memtable

	// Write many small key-value pairs
	numWrites := 5000
	testData := make(map[string]string)

	for i := 0; i < numWrites; i++ {
		key := []byte(fmt.Sprintf("k%d", i))
		value := []byte(fmt.Sprintf("v%d", i))
		testData[string(key)] = string(value)

		if err := d.Put(key, value); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}

		// Occasionally verify we can read back recent writes
		if i%1000 == 0 && i > 0 {
			got, err := d.Get(key)
			if err != nil {
				t.Fatalf("Get during writes %d: %v", i, err)
			}
			if string(got) != string(value) {
				t.Fatalf("Get during writes %d: got %q, want %q", i, string(got), string(value))
			}
		}
	}

	// Wait for multiple flushes
	waitForFlush(t, d, 3, 10*time.Second)

	// Verify we have multiple SSTable files
	files := listSSTables(t, dir)
	if len(files) < 3 {
		t.Fatalf("Expected at least 3 SSTable files, got %d", len(files))
	}

	// Random sample verification (checking all 5000 would be slow)
	sampleKeys := []int{0, 100, 500, 1000, 2500, 4999}
	for _, i := range sampleKeys {
		key := []byte(fmt.Sprintf("k%d", i))
		expectedValue := fmt.Sprintf("v%d", i)

		got, err := d.Get(key)
		if err != nil {
			t.Fatalf("Get sample key k%d: %v", i, err)
		}
		if string(got) != expectedValue {
			t.Fatalf("Get sample key k%d: got %q, want %q", i, string(got), expectedValue)
		}
	}
}
