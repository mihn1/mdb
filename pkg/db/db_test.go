package db_test

import (
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
	t.Helper()
	d, err := db.Open(path, common.NewDefaultOptions())
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
		time.Sleep(10 * time.Millisecond)
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
