package binary

import (
	"encoding/binary"
	"io"
	"math"
)

func NewDecoder(input io.Reader) *Decoder {
	return &Decoder{
		input:         input,
		compressInput: NewCompressReader(input),
	}
}

type Decoder struct {
	compress      bool
	input         io.Reader
	compressInput io.Reader
	scratch       [binary.MaxVarintLen64]byte
}

func (decoder *Decoder) SelectCompress(compress bool) {
	decoder.compress = compress
}

func (decoder *Decoder) Get() io.Reader {
	if decoder.compress {
		return decoder.compressInput
	}
	return decoder.input
}

func (decoder *Decoder) Bool() (bool, error) {
	v, err := decoder.ReadByte()
	if err != nil {
		return false, err
	}
	return v == 1, nil
}

func (decoder *Decoder) Uvarint() (uint64, error) {
	return binary.ReadUvarint(decoder)
}

func (decoder *Decoder) Int8() (int8, error) {
	v, err := decoder.ReadByte()
	if err != nil {
		return 0, err
	}
	return int8(v), nil
}

func (decoder *Decoder) Int16() (int16, error) {
	v, err := decoder.UInt16()
	if err != nil {
		return 0, err
	}
	return int16(v), nil
}

func (decoder *Decoder) Int32() (int32, error) {
	v, err := decoder.UInt32()
	if err != nil {
		return 0, err
	}
	return int32(v), nil
}

func (decoder *Decoder) Int64() (int64, error) {
	v, err := decoder.UInt64()
	if err != nil {
		return 0, err
	}
	return int64(v), nil
}

func (decoder *Decoder) UInt8() (uint8, error) {
	v, err := decoder.ReadByte()
	if err != nil {
		return 0, err
	}
	return uint8(v), nil
}

func (decoder *Decoder) UInt16() (uint16, error) {
	if _, err := decoder.Get().Read(decoder.scratch[:2]); err != nil {
		return 0, err
	}
	return uint16(decoder.scratch[0]) | uint16(decoder.scratch[1])<<8, nil
}

func (decoder *Decoder) UInt32() (uint32, error) {
	if _, err := decoder.Get().Read(decoder.scratch[:4]); err != nil {
		return 0, err
	}
	return uint32(decoder.scratch[0]) |
		uint32(decoder.scratch[1])<<8 |
		uint32(decoder.scratch[2])<<16 |
		uint32(decoder.scratch[3])<<24, nil
}

func (decoder *Decoder) UInt64() (uint64, error) {
	if _, err := decoder.Get().Read(decoder.scratch[:8]); err != nil {
		return 0, err
	}
	return uint64(decoder.scratch[0]) |
		uint64(decoder.scratch[1])<<8 |
		uint64(decoder.scratch[2])<<16 |
		uint64(decoder.scratch[3])<<24 |
		uint64(decoder.scratch[4])<<32 |
		uint64(decoder.scratch[5])<<40 |
		uint64(decoder.scratch[6])<<48 |
		uint64(decoder.scratch[7])<<56, nil
}

func (decoder *Decoder) Float32() (float32, error) {
	v, err := decoder.UInt32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(v), nil
}

func (decoder *Decoder) Float64() (float64, error) {
	v, err := decoder.UInt64()
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(v), nil
}

func (decoder *Decoder) Fixed(ln int) ([]byte, error) {
	if reader, ok := decoder.Get().(FixedReader); ok {
		return reader.Fixed(ln)
	}
	buf := make([]byte, ln)
	if _, err := decoder.Get().Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (decoder *Decoder) String() (string, error) {
	strlen, err := decoder.Uvarint()
	if err != nil {
		return "", err
	}
	str, err := decoder.Fixed(int(strlen))
	if err != nil {
		return "", err
	}
	return string(str), nil
}

func (decoder *Decoder) ReadByte() (byte, error) {
	if _, err := decoder.Get().Read(decoder.scratch[:1]); err != nil {
		return 0x0, err
	}
	return decoder.scratch[0], nil
}

type FixedReader interface {
	Fixed(ln int) ([]byte, error)
}
