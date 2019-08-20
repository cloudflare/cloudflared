package column

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kshvakov/clickhouse/lib/binary"
)

// Table of powers of 10 for fast casting from floating types to decimal type
// representations.
var factors10 = []float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13,
	1e14, 1e15, 1e16, 1e17, 1e18,
}

// Decimal represents Decimal(P, S) ClickHouse. Since there is support for
// int128 in Golang, the implementation does not support to 128-bits decimals
// as well. Decimal is represented as integral. Also floating-point types are
// supported for query parameters.
type Decimal struct {
	base
	nobits    int // its domain is {32, 64}
	precision int
	scale     int
}

func (d *Decimal) Read(decoder *binary.Decoder) (interface{}, error) {
	switch d.nobits {
	case 32:
		return decoder.Int32()
	case 64:
		return decoder.Int64()
	default:
		return nil, errors.New("unachievable execution path")
	}
}

func (d *Decimal) Write(encoder *binary.Encoder, v interface{}) error {
	switch d.nobits {
	case 32:
		return d.write32(encoder, v)
	case 64:
		return d.write64(encoder, v)
	default:
		return errors.New("unachievable execution path")
	}
}

func (d *Decimal) float2int32(floating float64) int32 {
	fixed := int32(floating * factors10[d.scale])
	return fixed
}

func (d *Decimal) float2int64(floating float64) int64 {
	fixed := int64(floating * factors10[d.scale])
	return fixed
}

func (d *Decimal) write32(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case int8:
		return encoder.Int32(int32(v))
	case int16:
		return encoder.Int32(int32(v))
	case int32:
		return encoder.Int32(int32(v))
	case int64:
		return errors.New("narrowing type conversion from int64 to int32")

	case uint8:
		return encoder.Int32(int32(v))
	case uint16:
		return encoder.Int32(int32(v))
	case uint32:
		return errors.New("narrowing type conversion from uint32 to int32")
	case uint64:
		return errors.New("narrowing type conversion from uint64 to int32")

	case float32:
		fixed := d.float2int32(float64(v))
		return encoder.Int32(fixed)
	case float64:
		fixed := d.float2int32(float64(v))
		return encoder.Int32(fixed)

	// this relies on Nullable never sending nil values through
	case *int8:
		return encoder.Int32(int32(*v))
	case *int16:
		return encoder.Int32(int32(*v))
	case *int32:
		return encoder.Int32(int32(*v))
	case *int64:
		return errors.New("narrowing type conversion from int64 to int32")

	case *uint8:
		return encoder.Int32(int32(*v))
	case *uint16:
		return encoder.Int32(int32(*v))
	case *uint32:
		return errors.New("narrowing type conversion from uint32 to int32")
	case *uint64:
		return errors.New("narrowing type conversion from uint64 to int32")

	case *float32:
		fixed := d.float2int32(float64(*v))
		return encoder.Int32(fixed)
	case *float64:
		fixed := d.float2int32(float64(*v))
		return encoder.Int32(fixed)
	}

	return &ErrUnexpectedType{
		T:      v,
		Column: d,
	}
}

func (d *Decimal) write64(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case int:
		return encoder.Int64(int64(v))
	case int8:
		return encoder.Int64(int64(v))
	case int16:
		return encoder.Int64(int64(v))
	case int32:
		return encoder.Int64(int64(v))
	case int64:
		return encoder.Int64(int64(v))

	case uint8:
		return encoder.Int64(int64(v))
	case uint16:
		return encoder.Int64(int64(v))
	case uint32:
		return encoder.Int64(int64(v))
	case uint64:
		return errors.New("narrowing type conversion from uint64 to int64")

	case float32:
		fixed := d.float2int64(float64(v))
		return encoder.Int64(fixed)
	case float64:
		fixed := d.float2int64(float64(v))
		return encoder.Int64(fixed)

	// this relies on Nullable never sending nil values through
	case *int:
		return encoder.Int64(int64(*v))
	case *int8:
		return encoder.Int64(int64(*v))
	case *int16:
		return encoder.Int64(int64(*v))
	case *int32:
		return encoder.Int64(int64(*v))
	case *int64:
		return encoder.Int64(int64(*v))

	case *uint8:
		return encoder.Int64(int64(*v))
	case *uint16:
		return encoder.Int64(int64(*v))
	case *uint32:
		return encoder.Int64(int64(*v))
	case *uint64:
		return errors.New("narrowing type conversion from uint64 to int64")

	case *float32:
		fixed := d.float2int64(float64(*v))
		return encoder.Int64(fixed)
	case *float64:
		fixed := d.float2int64(float64(*v))
		return encoder.Int64(fixed)
	}

	return &ErrUnexpectedType{
		T:      v,
		Column: d,
	}
}

func parseDecimal(name, chType string) (Column, error) {
	switch {
	case len(chType) < 12:
		fallthrough
	case !strings.HasPrefix(chType, "Decimal"):
		fallthrough
	case chType[7] != '(':
		fallthrough
	case chType[len(chType)-1] != ')':
		return nil, fmt.Errorf("invalid Decimal format: '%s'", chType)
	}

	var params = strings.Split(chType[8:len(chType)-1], ",")

	if len(params) != 2 {
		return nil, fmt.Errorf("invalid Decimal format: '%s'", chType)
	}

	params[0] = strings.TrimSpace(params[0])
	params[1] = strings.TrimSpace(params[1])

	var err error
	var decimal = &Decimal{
		base: base{
			name:   name,
			chType: chType,
		},
	}

	if decimal.precision, err = strconv.Atoi(params[0]); err != nil {
		return nil, fmt.Errorf("'%s' is not Decimal type: %s", chType, err)
	} else if decimal.precision < 1 {
		return nil, errors.New("wrong precision of Decimal type")
	}

	if decimal.scale, err = strconv.Atoi(params[1]); err != nil {
		return nil, fmt.Errorf("'%s' is not Decimal type: %s", chType, err)
	} else if decimal.scale < 0 || decimal.scale > decimal.precision {
		return nil, errors.New("wrong scale of Decimal type")
	}

	switch {
	case decimal.precision <= 9:
		decimal.nobits = 32
		decimal.valueOf = baseTypes[int32(0)]
	case decimal.precision <= 18:
		decimal.nobits = 64
		decimal.valueOf = baseTypes[int64(0)]
	case decimal.precision <= 38:
		return nil, errors.New("Decimal128 is not supported")
	default:
		return nil, errors.New("precision of Decimal exceeds max bound")
	}

	return decimal, nil
}
