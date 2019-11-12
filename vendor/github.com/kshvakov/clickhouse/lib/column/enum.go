package column

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kshvakov/clickhouse/lib/binary"
)

type Enum struct {
	iv map[string]interface{}
	vi map[interface{}]string
	base
	baseType interface{}
}

func (enum *Enum) Read(decoder *binary.Decoder) (interface{}, error) {
	var (
		err   error
		ident interface{}
	)
	switch enum.baseType.(type) {
	case int16:
		if ident, err = decoder.Int16(); err != nil {
			return nil, err
		}
	default:
		if ident, err = decoder.Int8(); err != nil {
			return nil, err
		}
	}
	if ident, found := enum.vi[ident]; found {
		return ident, nil
	}
	return nil, fmt.Errorf("invalid Enum value: %v", ident)
}

func (enum *Enum) Write(encoder *binary.Encoder, v interface{}) error {
	switch v := v.(type) {
	case string:
		ident, found := enum.iv[v]
		if !found {
			return fmt.Errorf("invalid Enum ident: %s", v)
		}
		switch ident := ident.(type) {
		case int8:
			return encoder.Int8(ident)
		case int16:
			return encoder.Int16(ident)
		}
	case uint8:
		if _, ok := enum.baseType.(int8); ok {
			return encoder.Int8(int8(v))
		}
	case int8:
		if _, ok := enum.baseType.(int8); ok {
			return encoder.Int8(v)
		}
	case uint16:
		if _, ok := enum.baseType.(int16); ok {
			return encoder.Int16(int16(v))
		}
	case int16:
		if _, ok := enum.baseType.(int16); ok {
			return encoder.Int16(v)
		}
	case int64:
		switch enum.baseType.(type) {
		case int8:
			return encoder.Int8(int8(v))
		case int16:
			return encoder.Int16(int16(v))
		}
	}
	return &ErrUnexpectedType{
		T:      v,
		Column: enum,
	}
}

func (enum *Enum) defaultValue() interface{} {
	return enum.baseType
}

func parseEnum(name, chType string) (*Enum, error) {
	var (
		data     string
		isEnum16 bool
	)
	if len(chType) < 8 {
		return nil, fmt.Errorf("invalid Enum format: %s", chType)
	}
	switch {
	case strings.HasPrefix(chType, "Enum8"):
		data = chType[6:]
	case strings.HasPrefix(chType, "Enum16"):
		data = chType[7:]
		isEnum16 = true
	default:
		return nil, fmt.Errorf("'%s' is not Enum type", chType)
	}
	enum := Enum{
		base: base{
			name:    name,
			chType:  chType,
			valueOf: baseTypes[string("")],
		},
		iv: make(map[string]interface{}),
		vi: make(map[interface{}]string),
	}
	for _, block := range strings.Split(data[:len(data)-1], ",") {
		parts := strings.Split(block, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid Enum format: %s", chType)
		}
		var (
			ident      = strings.TrimSpace(parts[0])
			value, err = strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 16)
		)
		if err != nil {
			return nil, fmt.Errorf("invalid Enum value: %v", chType)
		}
		{
			var (
				ident             = ident[1 : len(ident)-1]
				value interface{} = int16(value)
			)
			if !isEnum16 {
				value = int8(value.(int16))
			}
			if enum.baseType == nil {
				enum.baseType = value
			}
			enum.iv[ident] = value
			enum.vi[value] = ident
		}
	}
	return &enum, nil
}
