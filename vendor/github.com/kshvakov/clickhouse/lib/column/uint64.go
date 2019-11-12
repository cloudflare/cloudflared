package column

import (
	"github.com/kshvakov/clickhouse/lib/binary"
)

type UInt64 struct{ base }

func (UInt64) Read(decoder *binary.Decoder) (interface{}, error) {
	v, err := decoder.UInt64()
	if err != nil {
		return uint64(0), err
	}
	return v, nil
}

func (u *UInt64) Write(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case []byte:
		if _, err := encoder.Write(v); err != nil {
			return err
		}
		return nil
	case uint64:
		return encoder.UInt64(v)
	case int64:
		return encoder.UInt64(uint64(v))
	case int:
		return encoder.UInt64(uint64(v))

	// this relies on Nullable never sending nil values through
	case *uint64:
		return encoder.UInt64(*v)
	case *int64:
		return encoder.UInt64(uint64(*v))
	case *int:
		return encoder.UInt64(uint64(*v))
	}

	return &ErrUnexpectedType{
		T:      v,
		Column: u,
	}
}
