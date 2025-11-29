package db

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/mihn1/mdb/pkg/common"
)

func TestWALAppendReplaceAndRead(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, walFileName)

	wal, err := openWAL(walPath, nil)
	if err != nil {
		t.Fatalf("openWAL: %v", err)
	}
	defer wal.Close()

	entries := []*WALEntry{
		{Type: EntryTypePut, Key: []byte("alpha"), Value: []byte("value-alpha")},
		{Type: EntryTypeDelete, Key: []byte("beta")},
		{Type: EntryTypePut, Key: []byte("gamma"), Value: []byte("value-gamma")},
	}

	for _, e := range entries {
		if err := wal.Append(e); err != nil {
			t.Fatalf("append %q: %v", e.Key, err)
		}
	}
	if err := wal.Sync(); err != nil {
		t.Fatalf("wal sync: %v", err)
	}

	file, err := os.Open(walPath)
	if err != nil {
		t.Fatalf("open wal for read: %v", err)
	}
	reader := NewReader(file)
	var readEntries []*WALEntry
	for {
		entry, err := reader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("reader.Next: %v", err)
		}
		readEntries = append(readEntries, entry)
	}
	file.Close()

	if len(readEntries) != len(entries) {
		t.Fatalf("expected %d entries, got %d", len(entries), len(readEntries))
	}
	for i, e := range entries {
		got := readEntries[i]
		if e.Type != got.Type || string(e.Key) != string(got.Key) || string(e.Value) != string(got.Value) {
			t.Fatalf("entry %d mismatch: want %+v got %+v", i, e, got)
		}
	}

	trimmed := []*WALEntry{entries[2]}
	if err := wal.Replace(trimmed); err != nil {
		t.Fatalf("wal replace: %v", err)
	}
	if err := wal.Sync(); err != nil {
		t.Fatalf("wal sync after replace: %v", err)
	}

	file2, err := os.Open(walPath)
	if err != nil {
		t.Fatalf("open wal for read after replace: %v", err)
	}
	reader2 := NewReader(file2)
	entry, err := reader2.Next()
	if err != nil {
		t.Fatalf("reader2.Next: %v", err)
	}
	if string(entry.Key) != string(trimmed[0].Key) || string(entry.Value) != string(trimmed[0].Value) || entry.Type != trimmed[0].Type {
		t.Fatalf("unexpected entry after replace: %+v", entry)
	}
	if _, err := reader2.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after single entry, got %v", err)
	}
	file2.Close()
}

func TestWALRecoveryAppliesPendingEntries(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, walFileName)

	wal, err := openWAL(walPath, nil)
	if err != nil {
		t.Fatalf("openWAL: %v", err)
	}

	pending := []*WALEntry{
		{Type: EntryTypePut, Key: []byte("user:1"), Value: []byte("alice")},
		{Type: EntryTypePut, Key: []byte("user:2"), Value: []byte("bob")},
		{Type: EntryTypeDelete, Key: []byte("user:gone")},
	}
	for _, e := range pending {
		if err := wal.Append(e); err != nil {
			t.Fatalf("append pending: %v", err)
		}
	}
	if err := wal.Sync(); err != nil {
		t.Fatalf("wal sync: %v", err)
	}
	if err := wal.Close(); err != nil {
		t.Fatalf("wal close: %v", err)
	}

	opts := common.NewDefaultOptions()
	opts.MemTableSize = 2 << 20 // keep memtable large to avoid flush during recovery

	recovered, err := Open(dir, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer recovered.Close()

	value, err := recovered.Get([]byte("user:1"))
	if err != nil {
		t.Fatalf("Get user:1: %v", err)
	}
	if string(value) != "alice" {
		t.Fatalf("unexpected value for user:1: %q", value)
	}

	value, err = recovered.Get([]byte("user:2"))
	if err != nil {
		t.Fatalf("Get user:2: %v", err)
	}
	if string(value) != "bob" {
		t.Fatalf("unexpected value for user:2: %q", value)
	}

	value, err = recovered.Get([]byte("user:gone"))
	if err != nil {
		t.Fatalf("Get user:gone: %v", err)
	}
	if value != nil {
		t.Fatalf("expected user:gone to be deleted, got %q", value)
	}
}
