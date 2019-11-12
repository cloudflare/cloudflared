package column

import (
	"github.com/kshvakov/clickhouse/lib/binary"
)

type Int32 struct{ base }

func (Int32) Read(decoder *binary.Decoder) (interface{}, error) {
	v, err := decoder.Int32()
	if err != nil {
		return int32(0), err
	}
	return v, nil
}

func (i *Int32) Write(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case int32:
		return encoder.Int32(v)
	case int64:
		return encoder.Int32(int32(v))
	case int:
		return encoder.Int32(int32(v))

	// this relies on Nullable never sending nil values through
	case *int32:
		return encoder.Int32(*v)
	case *int64:
		return encoder.Int32(int32(*v))
	case *int:
		return encoder.Int32(int32(*v))
	}

	return &ErrUnexpectedType{
		T:      v,
		Column: i,
	}
}
