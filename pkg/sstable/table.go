package sstable

import (
	"fmt"
	"os"

	"github.com/mihn1/mdb/pkg/common"
)

// a table represents an immutable on-disk sorted string table.
// a table can be read by multiple goroutines concurrently without external synchronization.
type Table struct {
	file       *os.File
	size       int64
	indexBlock *Block
	footer     *footer
	opts       *common.Options
	filter     *filterBlockReader
}

func Open(f *os.File, opts *common.Options) (*Table, error) {
	t := &Table{
		file: f,
		opts: opts,
	}

	// Read footer
	if err := t.readFooter(); err != nil {
		return t, err
	}

	// Read index block
	indexBlockMeta := t.footer.indexBlockMeta
	indexBlock, err := t.readBlock(indexBlockMeta)
	if err != nil {
		return t, err
	}
	t.indexBlock = indexBlock

	if meta := t.footer.filterBlockMeta; meta != nil && meta.size > 0 {
		data, err := t.readRaw(meta)
		if err != nil {
			return t, err
		}
		filterReader, err := newFilterBlockReader(data)
		if err != nil {
			return t, err
		}
		t.filter = filterReader
	}
	return t, nil
}

func (t *Table) readBlock(blockMeta *blockMeta) (*Block, error) {
	data, err := t.readRaw(blockMeta)
	if err != nil {
		return nil, err
	}
	return &Block{
		data: data,
		opts: t.opts,
	}, nil
}

// Close releases resources held by the table.
func (t *Table) Close() error {
	if t == nil || t.file == nil {
		return nil
	}
	err := t.file.Close()
	t.file = nil
	return err
}

func (t *Table) readRaw(blockMeta *blockMeta) ([]byte, error) {
	// Validate block metadata before attempting to allocate memory
	if blockMeta.size > 1024*1024*10 { // 10MB sanity check
		return nil, fmt.Errorf("block size too large: %d bytes", blockMeta.size)
	}
	if blockMeta.size == 0 {
		return nil, fmt.Errorf("block size is zero")
	}
	if blockMeta.offset > uint64(t.size) {
		return nil, fmt.Errorf("block offset %d beyond file size %d", blockMeta.offset, t.size)
	}

	blockData := make([]byte, blockMeta.size)
	_, err := t.file.ReadAt(blockData, int64(blockMeta.offset))
	if err != nil {
		return nil, err
	}
	return blockData, nil
}

func (t *Table) readFooter() error {
	var footerSize int64 = footerEncodedLength
	buf := make([]byte, footerSize)

	// Get file size to calculate footer offset
	fileStat, err := t.file.Stat()
	if err != nil {
		return err
	}
	fileSize := fileStat.Size()
	t.size = fileSize // Set the size field

	// Read footer from the end of file
	footerOffset := fileSize - footerSize
	_, err = t.file.ReadAt(buf, footerOffset)
	if err != nil {
		return err
	}
	t.footer, err = decodeFooter(buf)
	return err
}

func (t *Table) Get(key []byte) (value []byte, err error) {
	if t.indexBlock == nil || len(t.indexBlock.data) == 0 {
		return nil, nil
	}

	var indexReader common.Reader = t.indexBlock.NewReader()
	indexReader.Seek(key)
	if !indexReader.Valid() {
		return nil, nil
	}
	blockIndex := -1
	if br, ok := indexReader.(*BlockReader); ok {
		blockIndex = br.EntryIndex()
	}
	if t.filter != nil && !t.filter.mayContain(blockIndex, key) {
		return nil, nil
	}

	indexBytes, err := indexReader.Value()
	if err != nil {
		return nil, err
	}
	dataBlockMeta, err := decodeBlockMeta(indexBytes)
	if err != nil {
		return nil, err
	}

	dataBlock, err := t.readBlock(dataBlockMeta)
	if err != nil {
		return nil, err
	}

	dataReader := dataBlock.NewReader()
	dataReader.Seek(key)
	if dataReader.Valid() {
		keyAt, err := dataReader.Key()
		// Check if the key matches the target key
		if err != nil || t.opts.Comparator.Compare(keyAt, key) != 0 {
			return nil, err
		}
		return dataReader.Value()
	}

	return nil, nil
}

// NewReader returns an iterator over all key/value pairs stored in the table.
func (t *Table) NewReader() common.Reader {
	indexIter := t.indexBlock.NewReader()
	it := &tableIterator{
		table:     t,
		indexIter: indexIter,
	}
	it.SeekToFirst()
	return it
}

type tableIterator struct {
	table     *Table
	indexIter common.Reader
	blockIter common.Reader
	err       error
}

func (it *tableIterator) Valid() bool {
	if it.err != nil || it.blockIter == nil {
		return false
	}
	return it.blockIter.Valid()
}

func (it *tableIterator) Key() ([]byte, error) {
	if it.err != nil {
		return nil, it.err
	}
	if it.blockIter == nil || !it.blockIter.Valid() {
		return nil, ErrInvalidBlockReader
	}
	return it.blockIter.Key()
}

func (it *tableIterator) Value() ([]byte, error) {
	if it.err != nil {
		return nil, it.err
	}
	if it.blockIter == nil || !it.blockIter.Valid() {
		return nil, ErrInvalidBlockReader
	}
	return it.blockIter.Value()
}

func (it *tableIterator) Next() {
	if it.err != nil || it.blockIter == nil {
		return
	}
	it.blockIter.Next()
	if it.blockIter.Valid() {
		return
	}
	it.indexIter.Next()
	it.loadCurrentBlock()
}

func (it *tableIterator) Seek(target []byte) {
	it.err = nil
	it.indexIter.SeekToFirst()
	cmp := it.table.opts.Comparator
	for it.indexIter.Valid() {
		keyBytes, err := it.indexIter.Key()
		if err != nil {
			it.err = err
			it.blockIter = nil
			return
		}
		if cmp.Compare(keyBytes, target) >= 0 {
			it.loadCurrentBlock()
			if it.err != nil {
				return
			}
			if it.blockIter != nil {
				it.blockIter.Seek(target)
				if it.blockIter.Valid() {
					return
				}
			}
		}
		it.indexIter.Next()
	}
	it.blockIter = nil
}

func (it *tableIterator) SeekToFirst() {
	it.err = nil
	it.indexIter.SeekToFirst()
	it.loadCurrentBlock()
}

func (it *tableIterator) loadCurrentBlock() {
	if !it.indexIter.Valid() {
		it.blockIter = nil
		return
	}
	valueBytes, err := it.indexIter.Value()
	if err != nil {
		it.err = err
		it.blockIter = nil
		return
	}
	meta, err := decodeBlockMeta(valueBytes)
	if err != nil {
		it.err = err
		it.blockIter = nil
		return
	}
	block, err := it.table.readBlock(meta)
	if err != nil {
		it.err = err
		it.blockIter = nil
		return
	}
	reader := block.NewReader()
	reader.SeekToFirst()
	it.blockIter = reader
}
