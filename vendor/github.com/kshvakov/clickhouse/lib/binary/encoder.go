package binary

import (
	"encoding/binary"
	"io"
	"math"
	"reflect"
	"unsafe"
)

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{
		output:         w,
		compressOutput: NewCompressWriter(w),
	}
}

type Encoder struct {
	compress       bool
	output         io.Writer
	compressOutput io.Writer
	scratch        [binary.MaxVarintLen64]byte
}

func (enc *Encoder) SelectCompress(compress bool) {
	if enc.compress && !compress {
		enc.Flush()
	}
	enc.compress = compress
}

func (enc *Encoder) Get() io.Writer {
	if enc.compress {
		return enc.compressOutput
	}
	return enc.output
}

func (enc *Encoder) Uvarint(v uint64) error {
	ln := binary.PutUvarint(enc.scratch[:binary.MaxVarintLen64], v)
	if _, err := enc.Get().Write(enc.scratch[0:ln]); err != nil {
		return err
	}
	return nil
}

func (enc *Encoder) Bool(v bool) error {
	if v {
		return enc.UInt8(1)
	}
	return enc.UInt8(0)
}

func (enc *Encoder) Int8(v int8) error {
	return enc.UInt8(uint8(v))
}

func (enc *Encoder) Int16(v int16) error {
	return enc.UInt16(uint16(v))
}

func (enc *Encoder) Int32(v int32) error {
	return enc.UInt32(uint32(v))
}

func (enc *Encoder) Int64(v int64) error {
	return enc.UInt64(uint64(v))
}

func (enc *Encoder) UInt8(v uint8) error {
	enc.scratch[0] = v
	if _, err := enc.Get().Write(enc.scratch[:1]); err != nil {
		return err
	}
	return nil
}

func (enc *Encoder) UInt16(v uint16) error {
	enc.scratch[0] = byte(v)
	enc.scratch[1] = byte(v >> 8)
	if _, err := enc.Get().Write(enc.scratch[:2]); err != nil {
		return err
	}
	return nil
}

func (enc *Encoder) UInt32(v uint32) error {
	enc.scratch[0] = byte(v)
	enc.scratch[1] = byte(v >> 8)
	enc.scratch[2] = byte(v >> 16)
	enc.scratch[3] = byte(v >> 24)
	if _, err := enc.Get().Write(enc.scratch[:4]); err != nil {
		return err
	}
	return nil
}

func (enc *Encoder) UInt64(v uint64) error {
	enc.scratch[0] = byte(v)
	enc.scratch[1] = byte(v >> 8)
	enc.scratch[2] = byte(v >> 16)
	enc.scratch[3] = byte(v >> 24)
	enc.scratch[4] = byte(v >> 32)
	enc.scratch[5] = byte(v >> 40)
	enc.scratch[6] = byte(v >> 48)
	enc.scratch[7] = byte(v >> 56)
	if _, err := enc.Get().Write(enc.scratch[:8]); err != nil {
		return err
	}
	return nil
}

func (enc *Encoder) Float32(v float32) error {
	return enc.UInt32(math.Float32bits(v))
}

func (enc *Encoder) Float64(v float64) error {
	return enc.UInt64(math.Float64bits(v))
}

func (enc *Encoder) String(v string) error {
	str := Str2Bytes(v)
	if err := enc.Uvarint(uint64(len(str))); err != nil {
		return err
	}
	if _, err := enc.Get().Write(str); err != nil {
		return err
	}
	return nil
}

func (enc *Encoder) RawString(str []byte) error {
	if err := enc.Uvarint(uint64(len(str))); err != nil {
		return err
	}
	if _, err := enc.Get().Write(str); err != nil {
		return err
	}
	return nil
}

func (enc *Encoder) Write(b []byte) (int, error) {
	return enc.Get().Write(b)
}

func (enc *Encoder) Flush() error {
	if w, ok := enc.Get().(WriteFlusher); ok {
		return w.Flush()
	}
	return nil
}

type WriteFlusher interface {
	Flush() error
}

func Str2Bytes(str string) []byte {
	header := (*reflect.SliceHeader)(unsafe.Pointer(&str))
	header.Len = len(str)
	header.Cap = header.Len
	return *(*[]byte)(unsafe.Pointer(header))
}
