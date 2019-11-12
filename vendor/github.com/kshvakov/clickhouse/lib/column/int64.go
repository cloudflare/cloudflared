package column

import (
	"github.com/kshvakov/clickhouse/lib/binary"
)

type Int64 struct{ base }

func (Int64) Read(decoder *binary.Decoder) (interface{}, error) {
	v, err := decoder.Int64()
	if err != nil {
		return int64(0), err
	}
	return v, nil
}

func (i *Int64) Write(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case int:
		return encoder.Int64(int64(v))
	case int64:
		return encoder.Int64(v)
	case []byte:
		if _, err := encoder.Write(v); err != nil {
			return err
		}
		return nil

	// this relies on Nullable never sending nil values through
	case *int:
		return encoder.Int64(int64(*v))
	case *int64:
		return encoder.Int64(*v)
	}

	return &ErrUnexpectedType{
		T:      v,
		Column: i,
	}
}
