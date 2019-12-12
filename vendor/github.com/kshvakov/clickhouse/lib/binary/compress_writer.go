// +build !clz4

package binary

import (
	"encoding/binary"
	"io"

	"github.com/kshvakov/clickhouse/lib/cityhash102"
	"github.com/kshvakov/clickhouse/lib/lz4"
)

type compressWriter struct {
	writer io.Writer
	// data uncompressed
	data []byte
	// data position
	pos int
	// data compressed
	zdata []byte
}

// NewCompressWriter wrap the io.Writer
func NewCompressWriter(w io.Writer) *compressWriter {
	p := &compressWriter{writer: w}
	p.data = make([]byte, BlockMaxSize, BlockMaxSize)

	zlen := lz4.CompressBound(BlockMaxSize) + HeaderSize
	p.zdata = make([]byte, zlen, zlen)
	return p
}

func (cw *compressWriter) Write(buf []byte) (int, error) {
	var n int
	for len(buf) > 0 {
		// Accumulate the data to be compressed.
		m := copy(cw.data[cw.pos:], buf)
		cw.pos += m
		buf = buf[m:]

		if cw.pos == len(cw.data) {
			err := cw.Flush()
			if err != nil {
				return n, err
			}
		}
		n += m
	}
	return n, nil
}

func (cw *compressWriter) Flush() (err error) {
	if cw.pos == 0 {
		return
	}

	// write the headers
	compressedSize, err := lz4.Encode(cw.zdata[HeaderSize:], cw.data[:cw.pos])
	if err != nil {
		return err
	}
	compressedSize += CompressHeaderSize
	// fill the header, compressed_size_32 + uncompressed_size_32
	cw.zdata[16] = LZ4
	binary.LittleEndian.PutUint32(cw.zdata[17:], uint32(compressedSize))
	binary.LittleEndian.PutUint32(cw.zdata[21:], uint32(cw.pos))

	// fill the checksum
	checkSum := cityhash102.CityHash128(cw.zdata[16:], uint32(compressedSize))
	binary.LittleEndian.PutUint64(cw.zdata[0:], checkSum.Lower64())
	binary.LittleEndian.PutUint64(cw.zdata[8:], checkSum.Higher64())

	cw.writer.Write(cw.zdata[:compressedSize+ChecksumSize])
	if w, ok := cw.writer.(WriteFlusher); ok {
		err = w.Flush()
	}
	cw.pos = 0
	return
}
