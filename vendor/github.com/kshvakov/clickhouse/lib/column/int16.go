package column

import (
	"github.com/kshvakov/clickhouse/lib/binary"
)

type Int16 struct{ base }

func (Int16) Read(decoder *binary.Decoder) (interface{}, error) {
	v, err := decoder.Int16()
	if err != nil {
		return int16(0), err
	}
	return v, nil
}

func (i *Int16) Write(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case int16:
		return encoder.Int16(v)
	case int64:
		return encoder.Int16(int16(v))
	case int:
		return encoder.Int16(int16(v))

	// this relies on Nullable never sending nil values through
	case *int16:
		return encoder.Int16(*v)
	case *int64:
		return encoder.Int16(int16(*v))
	case *int:
		return encoder.Int16(int16(*v))
	}

	return &ErrUnexpectedType{
		T:      v,
		Column: i,
	}
}
