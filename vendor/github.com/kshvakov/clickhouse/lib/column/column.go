package column

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kshvakov/clickhouse/lib/binary"
)

type Column interface {
	Name() string
	CHType() string
	ScanType() reflect.Type
	Read(*binary.Decoder) (interface{}, error)
	Write(*binary.Encoder, interface{}) error
	defaultValue() interface{}
}

func Factory(name, chType string, timezone *time.Location) (Column, error) {
	switch chType {
	case "Int8":
		return &Int8{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[int8(0)],
			},
		}, nil
	case "Int16":
		return &Int16{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[int16(0)],
			},
		}, nil
	case "Int32":
		return &Int32{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[int32(0)],
			},
		}, nil
	case "Int64":
		return &Int64{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[int64(0)],
			},
		}, nil
	case "UInt8":
		return &UInt8{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[uint8(0)],
			},
		}, nil
	case "UInt16":
		return &UInt16{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[uint16(0)],
			},
		}, nil
	case "UInt32":
		return &UInt32{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[uint32(0)],
			},
		}, nil
	case "UInt64":
		return &UInt64{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[uint64(0)],
			},
		}, nil
	case "Float32":
		return &Float32{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[float32(0)],
			},
		}, nil
	case "Float64":
		return &Float64{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[float64(0)],
			},
		}, nil
	case "String":
		return &String{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[string("")],
			},
		}, nil
	case "UUID":
		return &UUID{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[string("")],
			},
		}, nil
	case "Date":
		_, offset := time.Unix(0, 0).In(timezone).Zone()
		return &Date{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[time.Time{}],
			},
			Timezone: timezone,
			offset:   int64(offset),
		}, nil
	case "DateTime":
		return &DateTime{
			base: base{
				name:    name,
				chType:  chType,
				valueOf: baseTypes[time.Time{}],
			},
			Timezone: timezone,
		}, nil
	case "IPv4":
		return &IPv4{
			base: base{
				name:   name,
				chType: chType,
			},
		}, nil
	case "IPv6":
		return &IPv6{
			base: base{
				name:   name,
				chType: chType,
			},
		}, nil
	}

	switch {
	case strings.HasPrefix(chType, "Array"):
		return parseArray(name, chType, timezone)
	case strings.HasPrefix(chType, "Nullable"):
		return parseNullable(name, chType, timezone)
	case strings.HasPrefix(chType, "FixedString"):
		return parseFixedString(name, chType)
	case strings.HasPrefix(chType, "Enum8"), strings.HasPrefix(chType, "Enum16"):
		return parseEnum(name, chType)
	case strings.HasPrefix(chType, "Decimal"):
		return parseDecimal(name, chType)
	}
	return nil, fmt.Errorf("column: unhandled type %v", chType)
}
