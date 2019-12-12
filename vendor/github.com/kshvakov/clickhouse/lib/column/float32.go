package column

import (
	"github.com/kshvakov/clickhouse/lib/binary"
)

type Float32 struct{ base }

func (Float32) Read(decoder *binary.Decoder) (interface{}, error) {
	v, err := decoder.Float32()
	if err != nil {
		return float32(0), err
	}
	return v, nil
}

func (float *Float32) Write(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case float32:
		return encoder.Float32(v)
	case float64:
		return encoder.Float32(float32(v))

	// this relies on Nullable never sending nil values through
	case *float32:
		return encoder.Float32(*v)
	case *float64:
		return encoder.Float32(float32(*v))
	}

	return &ErrUnexpectedType{
		T:      v,
		Column: float,
	}
}
