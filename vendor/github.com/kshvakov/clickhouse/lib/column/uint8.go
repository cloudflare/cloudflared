package column

import (
	"github.com/kshvakov/clickhouse/lib/binary"
)

type UInt8 struct{ base }

func (UInt8) Read(decoder *binary.Decoder) (interface{}, error) {
	v, err := decoder.UInt8()
	if err != nil {
		return uint8(0), err
	}
	return v, nil
}

func (u *UInt8) Write(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case bool:
		return encoder.Bool(v)
	case uint8:
		return encoder.UInt8(v)
	case int64:
		return encoder.UInt8(uint8(v))
	case int:
		return encoder.UInt8(uint8(v))

	// this relies on Nullable never sending nil values through
	case *bool:
		return encoder.Bool(*v)
	case *uint8:
		return encoder.UInt8(*v)
	case *int64:
		return encoder.UInt8(uint8(*v))
	case *int:
		return encoder.UInt8(uint8(*v))
	}

	return &ErrUnexpectedType{
		T:      v,
		Column: u,
	}
}
