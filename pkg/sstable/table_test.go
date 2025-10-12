package sstable

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/mihn1/mdb/pkg/common"
)

func TestWriterBasic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "table.sst")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	defer file.Close()

	w, err := NewTableBuilder(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	// Insert a few sorted keys
	testSize := 500
	var keys []string = make([]string, 0, testSize)
	for i := 0; i < testSize; i++ {
		keys = append(keys, strconv.Itoa(i))
	}

	slices.Sort(keys) // Ensure keys are sorted

	for _, key := range keys {
		if err := w.Add([]byte(key), []byte("value-"+key)); err != nil {
			t.Fatalf("Add %s: %v", key, err)
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

func TestTableReader(t *testing.T) {
	// Create a test SSTable file first
	dir := t.TempDir()
	path := filepath.Join(dir, "test_reader.sst")

	// Write test data
	testData := map[string]string{
		"apple":      "fruit1",
		"banana":     "fruit2",
		"cherry":     "fruit3",
		"date":       "fruit4",
		"elderberry": "fruit5",
	}

	// Create the SSTable
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	w, err := NewTableBuilder(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("NewTableBuilder: %v", err)
	}

	// Add keys in sorted order
	keys := []string{"apple", "banana", "cherry", "date", "elderberry"}
	for _, key := range keys {
		if err := w.Add([]byte(key), []byte(testData[key])); err != nil {
			t.Fatalf("Add %s: %v", key, err)
		}
	}

	if err := w.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	file.Close()

	// Now test reading
	file, err = os.Open(path)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer file.Close()

	table, err := Open(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("Open table: %v", err)
	}

	// Test Get existing keys
	for key, expectedValue := range testData {
		value, err := table.Get([]byte(key))
		if err != nil {
			t.Fatalf("Get %s: %v", key, err)
		}
		if string(value) != expectedValue {
			t.Fatalf("Get %s: got %q, want %q", key, string(value), expectedValue)
		}
	}

	// Test Get non-existing key
	value, err := table.Get([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("Get nonexistent: %v", err)
	}
	if value != nil {
		t.Fatalf("Get nonexistent: got %q, want nil", string(value))
	}
}

func TestTableReaderLargeDataset(t *testing.T) {
	// Create a test SSTable with many entries to test block boundaries
	dir := t.TempDir()
	path := filepath.Join(dir, "test_large.sst")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	w, err := NewTableBuilder(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("NewTableBuilder: %v", err)
	}

	// Create a large dataset that will span multiple blocks
	testSize := 1000
	testData := make(map[string]string)
	var keys []string

	for i := 0; i < testSize; i++ {
		key := "key" + strconv.Itoa(i)
		value := "value" + strconv.Itoa(i) + "_" + key
		keys = append(keys, key)
		testData[key] = value
	}

	slices.Sort(keys) // Ensure keys are sorted

	for _, key := range keys {
		if err := w.Add([]byte(key), []byte(testData[key])); err != nil {
			t.Fatalf("Add %s: %v", key, err)
		}
	}

	if err := w.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	file.Close()

	// Now test reading
	file, err = os.Open(path)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer file.Close()

	table, err := Open(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("Open table: %v", err)
	}

	// Test random access to various keys
	testKeys := []string{"key0", "key100", "key500", "key999", "key250", "key750"}
	for _, key := range testKeys {
		expectedValue := testData[key]
		value, err := table.Get([]byte(key))
		if err != nil {
			t.Fatalf("Get %s: %v", key, err)
		}
		if string(value) != expectedValue {
			t.Fatalf("Get %s: got %q, want %q", key, string(value), expectedValue)
		}
	}

	// Test all keys to ensure none are lost
	for _, key := range keys {
		expectedValue := testData[key]
		value, err := table.Get([]byte(key))
		if err != nil {
			t.Fatalf("Get %s: %v", key, err)
		}
		if string(value) != expectedValue {
			t.Fatalf("Get %s: got %q, want %q", key, string(value), expectedValue)
		}
	}
}

func TestBlockReader(t *testing.T) {
	// Test the block reader directly
	dir := t.TempDir()
	path := filepath.Join(dir, "test_block.sst")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	w, err := NewTableBuilder(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("NewTableBuilder: %v", err)
	}

	// Add test data
	testData := []struct {
		key   string
		value string
	}{
		{"a", "value_a"},
		{"b", "value_b"},
		{"c", "value_c"},
		{"d", "value_d"},
	}

	for _, kv := range testData {
		if err := w.Add([]byte(kv.key), []byte(kv.value)); err != nil {
			t.Fatalf("Add %s: %v", kv.key, err)
		}
	}

	if err := w.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	file.Close()

	// Read the table and test block iteration
	file, err = os.Open(path)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer file.Close()

	table, err := Open(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("Open table: %v", err)
	}

	// Get the first data block through the index
	indexReader := table.indexBlock.NewReader()
	indexReader.SeekToFirst()

	if !indexReader.Valid() {
		t.Fatalf("Index block is empty")
	}

	indexValue, err := indexReader.Value()
	if err != nil {
		t.Fatalf("Index value: %v", err)
	}

	blockMeta, err := decodeBlockMeta(indexValue)
	if err != nil {
		t.Fatalf("Decode block meta: %v", err)
	}

	dataBlock, err := table.readBlock(blockMeta)
	if err != nil {
		t.Fatalf("Read data block: %v", err)
	}

	// Test block reader iteration
	reader := dataBlock.NewReader()

	// Test SeekToFirst
	reader.SeekToFirst()
	i := 0
	for reader.Valid() {
		if i >= len(testData) {
			t.Fatalf("Too many entries in block")
		}

		key, err := reader.Key()
		if err != nil {
			t.Fatalf("Key at position %d: %v", i, err)
		}

		value, err := reader.Value()
		if err != nil {
			t.Fatalf("Value at position %d: %v", i, err)
		}

		if string(key) != testData[i].key {
			t.Fatalf("Key at position %d: got %q, want %q", i, string(key), testData[i].key)
		}

		if string(value) != testData[i].value {
			t.Fatalf("Value at position %d: got %q, want %q", i, string(value), testData[i].value)
		}

		reader.Next()
		i++
	}

	if i != len(testData) {
		t.Fatalf("Expected %d entries, got %d", len(testData), i)
	}

	// Test Seek functionality
	reader.Seek([]byte("b"))
	if !reader.Valid() {
		t.Fatalf("Seek to 'b' failed")
	}

	key, err := reader.Key()
	if err != nil {
		t.Fatalf("Key after seek: %v", err)
	}

	if string(key) != "b" {
		t.Fatalf("Seek to 'b': got %q, want 'b'", string(key))
	}

	// Test seek to non-existing key (should position at next greater key)
	reader.Seek([]byte("bb"))
	if !reader.Valid() {
		t.Fatalf("Seek to 'bb' should position at 'c'")
	}

	key, err = reader.Key()
	if err != nil {
		t.Fatalf("Key after seek to 'bb': %v", err)
	}

	if string(key) != "c" {
		t.Fatalf("Seek to 'bb': got %q, want 'c'", string(key))
	}
}

func TestTableReaderEdgeCases(t *testing.T) {
	// Test empty table
	dir := t.TempDir()
	path := filepath.Join(dir, "test_empty.sst")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	w, err := NewTableBuilder(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("NewTableBuilder: %v", err)
	}

	// Finish without adding any data
	if err := w.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	file.Close()

	// Test reading empty table
	file, err = os.Open(path)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer file.Close()

	table, err := Open(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("Open empty table: %v", err)
	}

	// Test Get on empty table
	value, err := table.Get([]byte("any_key"))
	if err != nil {
		t.Fatalf("Get on empty table: %v", err)
	}
	if value != nil {
		t.Fatalf("Get on empty table: got %q, want nil", string(value))
	}

	// Test single entry table
	path = filepath.Join(dir, "test_single.sst")
	file, err = os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	w, err = NewTableBuilder(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("NewTableBuilder: %v", err)
	}

	if err := w.Add([]byte("single_key"), []byte("single_value")); err != nil {
		t.Fatalf("Add single entry: %v", err)
	}

	if err := w.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	file.Close()

	// Test reading single entry table
	file, err = os.Open(path)
	if err != nil {
		t.Fatalf("open file: %v", err)
	}
	defer file.Close()

	table, err = Open(file, common.NewDefaultOptions())
	if err != nil {
		t.Fatalf("Open single entry table: %v", err)
	}

	// Test Get existing key
	value, err = table.Get([]byte("single_key"))
	if err != nil {
		t.Fatalf("Get single key: %v", err)
	}
	if string(value) != "single_value" {
		t.Fatalf("Get single key: got %q, want 'single_value'", string(value))
	}

	// Test Get non-existing key
	value, err = table.Get([]byte("not_found"))
	if err != nil {
		t.Fatalf("Get non-existing key: %v", err)
	}
	if value != nil {
		t.Fatalf("Get non-existing key: got %q, want nil", string(value))
	}
}
