package column

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kshvakov/clickhouse/lib/binary"
)

type Array struct {
	base
	depth  int
	column Column
}

func (array *Array) Read(decoder *binary.Decoder) (interface{}, error) {
	return nil, fmt.Errorf("do not use Read method for Array(T) column")
}

func (array *Array) Write(encoder *binary.Encoder, v interface{}) error {
	return array.column.Write(encoder, v)
}

func (array *Array) ReadArray(decoder *binary.Decoder, rows int) (_ []interface{}, err error) {
	var (
		values  = make([]interface{}, rows)
		offsets = make([]uint64, rows)
	)
	for i := 0; i < rows; i++ {
		offset, err := decoder.UInt64()
		if err != nil {
			return nil, err
		}
		offsets[i] = offset
	}
	for n, offset := range offsets {
		ln := offset
		if n != 0 {
			ln = ln - offsets[n-1]
		}
		if values[n], err = array.read(decoder, int(ln)); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (array *Array) read(decoder *binary.Decoder, ln int) (interface{}, error) {
	slice := reflect.MakeSlice(array.valueOf.Type(), 0, ln)
	for i := 0; i < ln; i++ {
		value, err := array.column.Read(decoder)
		if err != nil {
			return nil, err
		}
		slice = reflect.Append(slice, reflect.ValueOf(value))
	}
	return slice.Interface(), nil
}

func parseArray(name, chType string, timezone *time.Location) (*Array, error) {
	if len(chType) < 11 {
		return nil, fmt.Errorf("invalid Array column type: %s", chType)
	}
	var (
		depth      int
		columnType = chType
	)

loop:
	for _, str := range strings.Split(chType, "Array(") {
		switch {
		case len(str) == 0:
			depth++
		default:
			chType = str[:len(str)-depth]
			break loop
		}
	}
	column, err := Factory(name, chType, timezone)
	if err != nil {
		return nil, fmt.Errorf("Array(T): %v", err)
	}

	var scanType interface{}
	switch t := column.ScanType().Kind(); t {
	case reflect.Int8:
		scanType = []int8{}
	case reflect.Int16:
		scanType = []int16{}
	case reflect.Int32:
		scanType = []int32{}
	case reflect.Int64:
		scanType = []int64{}
	case reflect.Uint8:
		scanType = []uint8{}
	case reflect.Uint16:
		scanType = []uint16{}
	case reflect.Uint32:
		scanType = []uint32{}
	case reflect.Uint64:
		scanType = []uint64{}
	case reflect.Float32:
		scanType = []float32{}
	case reflect.Float64:
		scanType = []float64{}
	case reflect.String:
		scanType = []string{}
	case baseTypes[time.Time{}].Kind():
		scanType = []time.Time{}
	default:
		return nil, fmt.Errorf("unsupported Array type '%s'", column.ScanType().Name())
	}
	return &Array{
		base: base{
			name:    name,
			chType:  columnType,
			valueOf: reflect.ValueOf(scanType),
		},
		depth:  depth,
		column: column,
	}, nil
}
