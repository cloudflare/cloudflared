package quicvarint

import (
	"fmt"
	"io"
)

// taken from the QUIC draft
const (
	// Min is the minimum value allowed for a QUIC varint.
	Min = 0

	// Max is the maximum allowed value for a QUIC varint (2^62-1).
	Max = maxVarInt8

	maxVarInt1 = 63
	maxVarInt2 = 16383
	maxVarInt4 = 1073741823
	maxVarInt8 = 4611686018427387903
)

// Read reads a number in the QUIC varint format from r.
func Read(r io.ByteReader) (uint64, error) {
	firstByte, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	// the first two bits of the first byte encode the length
	l := 1 << ((firstByte & 0xc0) >> 6)
	b1 := firstByte & (0xff - 0xc0)
	if l == 1 {
		return uint64(b1), nil
	}
	b2, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if l == 2 {
		return uint64(b2) + uint64(b1)<<8, nil
	}
	b3, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	b4, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	if l == 4 {
		return uint64(b4) + uint64(b3)<<8 + uint64(b2)<<16 + uint64(b1)<<24, nil
	}
	b5, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	b6, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	b7, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	b8, err := r.ReadByte()
	if err != nil {
		return 0, err
	}
	return uint64(b8) + uint64(b7)<<8 + uint64(b6)<<16 + uint64(b5)<<24 + uint64(b4)<<32 + uint64(b3)<<40 + uint64(b2)<<48 + uint64(b1)<<56, nil
}

// Parse reads a number in the QUIC varint format.
// It returns the number of bytes consumed.
func Parse(b []byte) (uint64 /* value */, int /* bytes consumed */, error) {
	if len(b) == 0 {
		return 0, 0, io.EOF
	}
	firstByte := b[0]
	// the first two bits of the first byte encode the length
	l := 1 << ((firstByte & 0xc0) >> 6)
	if len(b) < l {
		return 0, 0, io.ErrUnexpectedEOF
	}
	b0 := firstByte & (0xff - 0xc0)
	if l == 1 {
		return uint64(b0), 1, nil
	}
	if l == 2 {
		return uint64(b[1]) + uint64(b0)<<8, 2, nil
	}
	if l == 4 {
		return uint64(b[3]) + uint64(b[2])<<8 + uint64(b[1])<<16 + uint64(b0)<<24, 4, nil
	}
	return uint64(b[7]) + uint64(b[6])<<8 + uint64(b[5])<<16 + uint64(b[4])<<24 + uint64(b[3])<<32 + uint64(b[2])<<40 + uint64(b[1])<<48 + uint64(b0)<<56, 8, nil
}

// Append appends i in the QUIC varint format.
func Append(b []byte, i uint64) []byte {
	if i <= maxVarInt1 {
		return append(b, uint8(i))
	}
	if i <= maxVarInt2 {
		return append(b, []byte{uint8(i>>8) | 0x40, uint8(i)}...)
	}
	if i <= maxVarInt4 {
		return append(b, []byte{uint8(i>>24) | 0x80, uint8(i >> 16), uint8(i >> 8), uint8(i)}...)
	}
	if i <= maxVarInt8 {
		return append(b, []byte{
			uint8(i>>56) | 0xc0, uint8(i >> 48), uint8(i >> 40), uint8(i >> 32),
			uint8(i >> 24), uint8(i >> 16), uint8(i >> 8), uint8(i),
		}...)
	}
	panic(fmt.Sprintf("%#x doesn't fit into 62 bits", i))
}

// AppendWithLen append i in the QUIC varint format with the desired length.
func AppendWithLen(b []byte, i uint64, length int) []byte {
	if length != 1 && length != 2 && length != 4 && length != 8 {
		panic("invalid varint length")
	}
	l := Len(i)
	if l == length {
		return Append(b, i)
	}
	if l > length {
		panic(fmt.Sprintf("cannot encode %d in %d bytes", i, length))
	}
	if length == 2 {
		b = append(b, 0b01000000)
	} else if length == 4 {
		b = append(b, 0b10000000)
	} else if length == 8 {
		b = append(b, 0b11000000)
	}
	for j := 1; j < length-l; j++ {
		b = append(b, 0)
	}
	for j := 0; j < l; j++ {
		b = append(b, uint8(i>>(8*(l-1-j))))
	}
	return b
}

// Len determines the number of bytes that will be needed to write the number i.
func Len(i uint64) int {
	if i <= maxVarInt1 {
		return 1
	}
	if i <= maxVarInt2 {
		return 2
	}
	if i <= maxVarInt4 {
		return 4
	}
	if i <= maxVarInt8 {
		return 8
	}
	// Don't use a fmt.Sprintf here to format the error message.
	// The function would then exceed the inlining budget.
	panic(struct {
		message string
		num     uint64
	}{"value doesn't fit into 62 bits: ", i})
}
