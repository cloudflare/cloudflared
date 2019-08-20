package column

import (
	"fmt"
	"reflect"
	"time"

	"github.com/kshvakov/clickhouse/lib/binary"
)

type Nullable struct {
	base
	column Column
}

func (null *Nullable) ScanType() reflect.Type {
	return null.column.ScanType()
}

func (null *Nullable) Read(decoder *binary.Decoder) (interface{}, error) {
	return null.column.Read(decoder)
}

func (null *Nullable) Write(encoder *binary.Encoder, v interface{}) error {
	return nil
}

func (null *Nullable) ReadNull(decoder *binary.Decoder, rows int) (_ []interface{}, err error) {
	var (
		isNull byte
		value  interface{}
		nulls  = make([]byte, rows)
		values = make([]interface{}, rows)
	)
	for i := 0; i < rows; i++ {
		if isNull, err = decoder.ReadByte(); err != nil {
			return nil, err
		}
		nulls[i] = isNull
	}
	for i, isNull := range nulls {
		switch value, err = null.column.Read(decoder); true {
		case err != nil:
			return nil, err
		case isNull == 0:
			values[i] = value
		default:
			values[i] = nil
		}
	}
	return values, nil
}
func (null *Nullable) WriteNull(nulls, encoder *binary.Encoder, v interface{}) error {
	if value := reflect.ValueOf(v); v == nil || (value.Kind() == reflect.Ptr && value.IsNil()) {
		if _, err := nulls.Write([]byte{1}); err != nil {
			return err
		}
		return null.column.Write(encoder, null.column.defaultValue())
	}
	if _, err := nulls.Write([]byte{0}); err != nil {
		return err
	}
	return null.column.Write(encoder, v)
}

func parseNullable(name, chType string, timezone *time.Location) (*Nullable, error) {
	if len(chType) < 14 {
		return nil, fmt.Errorf("invalid Nullable column type: %s", chType)
	}
	column, err := Factory(name, chType[9:][:len(chType)-10], timezone)
	if err != nil {
		return nil, fmt.Errorf("Nullable(T): %v", err)
	}
	return &Nullable{
		base: base{
			name:   name,
			chType: chType,
		},
		column: column,
	}, nil
}
