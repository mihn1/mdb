package sstable

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestWriterBasic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "table.sst")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	// Insert a few sorted keys
	for i := 0; i < 50; i++ {
		k := []byte{byte(i)}
		v := []byte("val")
		if err := w.Add(k, v); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if err := w.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(data) < 28 {
		t.Fatalf("file too small: %d", len(data))
	}
	// Footer is last 28 bytes
	footer := data[len(data)-28:]
	magic := binary.LittleEndian.Uint64(footer[20:28])
	if magic != tableMagic {
		t.Fatalf("bad magic: got %x want %x", magic, tableMagic)
	}
	version := binary.LittleEndian.Uint32(footer[16:20])
	if version != tableVersion {
		t.Fatalf("bad version: got %d want %d", version, tableVersion)
	}
	indexOffset := binary.LittleEndian.Uint64(footer[0:8])
	indexLength := binary.LittleEndian.Uint64(footer[8:16])
	if indexOffset == 0 || indexLength == 0 {
		t.Fatalf("unexpected index metadata offset=%d length=%d", indexOffset, indexLength)
	}
	if int(indexOffset+indexLength) > len(data)-28 {
		t.Fatalf("index extends beyond file")
	}
}
