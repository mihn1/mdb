package sstable

import "encoding/binary"

// Table layout (little endian):
//   [DataBlocks]* [FilterBlock?] [IndexBlock] [Footer]
// Data blocks remain unchanged. When bloom filters are enabled a single
// filter block is appended before the index block. The filter block encodes:
//   uint32 filterCount   - number of data blocks / filters
//   uint32 filterBytes   - size of each fixed-length bloom bit array
//   uint32 hashFuncs     - hash probes per key
//   repeated filterCount byte slices (filterBytes each) storing the bits.
// The footer stores block metadata for both filter and index blocks so readers
// can locate them without scanning.

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

const footerEncodedLength = 44

type footer struct {
	filterBlockMeta *blockMeta
	indexBlockMeta  *blockMeta
	version         uint32
	magicNumber     uint64
}

func (f *footer) encode() []byte {
	buf := make([]byte, 0, footerEncodedLength)
	buf = append(buf, encodeOrZero(f.filterBlockMeta)...)
	buf = append(buf, encodeOrZero(f.indexBlockMeta)...)
	buf = binary.LittleEndian.AppendUint32(buf, f.version)
	buf = binary.LittleEndian.AppendUint64(buf, f.magicNumber)
	return buf
}

func decodeFooter(buf []byte) (*footer, error) {
	if len(buf) < footerEncodedLength {
		return nil, ErrInvalidFooter
	}

	filterBlockMeta, err := decodeBlockMeta(buf[:16])
	if err != nil {
		return nil, err
	}
	if filterBlockMeta.offset == 0 && filterBlockMeta.size == 0 {
		filterBlockMeta = nil
	}

	indexBlockMeta, err := decodeBlockMeta(buf[16:32])
	if err != nil {
		return nil, err
	}

	return &footer{
		filterBlockMeta: filterBlockMeta,
		indexBlockMeta:  indexBlockMeta,
		version:         binary.LittleEndian.Uint32(buf[32:36]),
		magicNumber:     binary.LittleEndian.Uint64(buf[36:44]),
	}, nil
}

func encodeOrZero(meta *blockMeta) []byte {
	if meta == nil {
		return make([]byte, 16)
	}
	return meta.encode()
}
