package sstable

import (
	"os"

	"github.com/mihn1/mdb/pkg/common"
)

// a table represents an immutable on-disk sorted string table.
// a table can be read by multiple goroutines concurrently without external synchronization.
type Table struct {
	file *os.File
	// size       int64
	indexBlock *Block
	footer     *footer
	opts       *common.Options
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
	return t, nil
}

func (t *Table) readBlock(blockMeta *blockMeta) (*Block, error) {
	blockData := make([]byte, blockMeta.size)
	_, err := t.file.ReadAt(blockData, int64(blockMeta.offset))
	if err != nil {
		return nil, err
	}
	return &Block{
		data: blockData,
		opts: t.opts,
	}, nil
}

func (t *Table) readFooter() error {
	var footerSize int64 = 28
	buf := make([]byte, footerSize)

	// Get file size to calculate footer offset
	fileStat, err := t.file.Stat()
	if err != nil {
		return err
	}
	fileSize := fileStat.Size()

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
	var indexReader common.Iterator = t.indexBlock.NewReader()
	indexReader.Seek(key)
	if !indexReader.Valid() {
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
