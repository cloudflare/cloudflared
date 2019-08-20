// +build clz4

package binary

import (
	"encoding/binary"
	"fmt"
	"io"

	lz4 "github.com/cloudflare/golz4"
)

type compressReader struct {
	reader io.Reader
	// data uncompressed
	data []byte
	// data position
	pos int
	// data compressed
	zdata []byte
	// lz4 headers
	header []byte
}

// NewCompressReader wrap the io.Reader
func NewCompressReader(r io.Reader) *compressReader {
	p := &compressReader{
		reader: r,
		header: make([]byte, HeaderSize),
	}
	p.data = make([]byte, BlockMaxSize, BlockMaxSize)

	zlen := lz4.CompressBound(p.data) + HeaderSize
	p.zdata = make([]byte, zlen, zlen)

	p.pos = len(p.data)
	return p
}

func (cr *compressReader) Read(buf []byte) (n int, err error) {
	var bytesRead = 0
	n = len(buf)

	if cr.pos < len(cr.data) {
		copyedSize := copy(buf, cr.data[cr.pos:])

		bytesRead += copyedSize
		cr.pos += copyedSize
	}

	for bytesRead < n {
		if err = cr.readCompressedData(); err != nil {
			return bytesRead, err
		}
		copyedSize := copy(buf[bytesRead:], cr.data)

		bytesRead += copyedSize
		cr.pos = copyedSize
	}
	return n, nil
}

func (cr *compressReader) readCompressedData() (err error) {
	cr.pos = 0
	var n int
	n, err = cr.reader.Read(cr.header)
	if err != nil {
		return
	}
	if n != len(cr.header) {
		return fmt.Errorf("Lz4 decompression header EOF")
	}

	compressedSize := int(binary.LittleEndian.Uint32(cr.header[17:])) - 9
	decompressedSize := int(binary.LittleEndian.Uint32(cr.header[21:]))

	if compressedSize > cap(cr.zdata) {
		cr.zdata = make([]byte, compressedSize)
	}
	if decompressedSize > cap(cr.data) {
		cr.data = make([]byte, decompressedSize)
	}

	cr.zdata = cr.zdata[:compressedSize]
	cr.data = cr.data[:decompressedSize]

	// @TODO checksum
	if cr.header[16] == LZ4 {
		n, err = cr.reader.Read(cr.zdata)
		if err != nil {
			return
		}

		if n != len(cr.zdata) {
			return fmt.Errorf("Decompress read size not match")
		}

		err = lz4.Uncompress(cr.zdata, cr.data)
		if err != nil {
			return
		}
	} else {
		return fmt.Errorf("Unknown compression method: 0x%02x ", cr.header[16])
	}

	return nil
}
