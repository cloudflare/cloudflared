package column

import (
	"github.com/kshvakov/clickhouse/lib/binary"
)

type String struct{ base }

func (String) Read(decoder *binary.Decoder) (interface{}, error) {
	v, err := decoder.String()
	if err != nil {
		return "", err
	}
	return v, nil
}

func (str *String) Write(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case string:
		return encoder.String(v)
	case []byte:
		return encoder.RawString(v)

	// this relies on Nullable never sending nil values through
	case *string:
		return encoder.String(*v)
	}

	return &ErrUnexpectedType{
		T:      v,
		Column: str,
	}
}
