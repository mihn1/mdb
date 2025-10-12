package sstable

import "encoding/binary"

type blockMeta struct {
	size   uint64
	offset uint64
}

func (bm *blockMeta) encode() []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint64(buf[:8], bm.size)
	binary.LittleEndian.PutUint64(buf[8:], bm.offset)
	return buf
}

// decodeBlockMeta reads a blockMeta from the given byte slice. No advance the input slice.
func decodeBlockMeta(buf []byte) (*blockMeta, error) {
	if len(buf) < 16 {
		return nil, ErrInvalidBlockMeta
	}

	bm := &blockMeta{
		size:   binary.LittleEndian.Uint64(buf[:8]),
		offset: binary.LittleEndian.Uint64(buf[8:16]),
	}

	return bm, nil
}

type footer struct {
	indexBlockMeta *blockMeta
	version        uint32
	magicNumber    uint64
}

func (f *footer) encode() []byte {
	buf := make([]byte, 28)
	binary.LittleEndian.PutUint64(buf[:8], f.indexBlockMeta.offset)
	binary.LittleEndian.PutUint64(buf[8:16], f.indexBlockMeta.size)
	binary.LittleEndian.PutUint32(buf[16:20], f.version)
	binary.LittleEndian.PutUint64(buf[20:], f.magicNumber)
	return buf
}

func decodeFooter(buf []byte) (*footer, error) {
	if len(buf) < 28 {
		return nil, ErrInvalidFooter
	}

	indexBlockMeta, err := decodeBlockMeta(buf[:16])
	if err != nil {
		return nil, err
	}

	return &footer{
		indexBlockMeta: indexBlockMeta,
		version:        binary.LittleEndian.Uint32(buf[16:20]),
		magicNumber:    binary.LittleEndian.Uint64(buf[20:28]),
	}, nil
}
