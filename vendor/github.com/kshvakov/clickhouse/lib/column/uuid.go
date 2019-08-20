package column

import (
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"

	"github.com/kshvakov/clickhouse/lib/binary"
)

const UUIDLen = 16

var ErrInvalidUUIDFormat = errors.New("invalid UUID format")

type UUID struct {
	base
	scanType reflect.Type
}

func (*UUID) Read(decoder *binary.Decoder) (interface{}, error) {
	src, err := decoder.Fixed(UUIDLen)
	if err != nil {
		return "", err
	}

	src = swap(src)

	var uuid [36]byte
	{
		hex.Encode(uuid[:], src[:4])
		uuid[8] = '-'
		hex.Encode(uuid[9:13], src[4:6])
		uuid[13] = '-'
		hex.Encode(uuid[14:18], src[6:8])
		uuid[18] = '-'
		hex.Encode(uuid[19:23], src[8:10])
		uuid[23] = '-'
		hex.Encode(uuid[24:], src[10:])
	}
	return string(uuid[:]), nil
}

func (u *UUID) Write(encoder *binary.Encoder, v interface{}) (err error) {
	var uuid []byte
	switch v := v.(type) {
	case string:
		if uuid, err = uuid2bytes(v); err != nil {
			return err
		}
	case []byte:
		if len(v) != UUIDLen {
			return fmt.Errorf("invalid raw UUID len '%s' (expected %d, got %d)", uuid, UUIDLen, len(uuid))
		}
		uuid = make([]byte, 16)
		copy(uuid, v)
	default:
		return &ErrUnexpectedType{
			T:      v,
			Column: u,
		}
	}

	uuid = swap(uuid)

	if _, err := encoder.Write(uuid); err != nil {
		return err
	}
	return nil
}

func swap(src []byte) []byte {
	_ = src[15]
	src[0], src[7] = src[7], src[0]
	src[1], src[6] = src[6], src[1]
	src[2], src[5] = src[5], src[2]
	src[3], src[4] = src[4], src[3]
	src[8], src[15] = src[15], src[8]
	src[9], src[14] = src[14], src[9]
	src[10], src[13] = src[13], src[10]
	src[11], src[12] = src[12], src[11]
	return src
}

func uuid2bytes(str string) ([]byte, error) {
	var uuid [16]byte
	if str[8] != '-' || str[13] != '-' || str[18] != '-' || str[23] != '-' {
		return nil, ErrInvalidUUIDFormat
	}
	for i, x := range [16]int{
		0, 2, 4, 6,
		9, 11, 14, 16,
		19, 21, 24, 26,
		28, 30, 32, 34,
	} {
		if v, ok := xtob(str[x], str[x+1]); !ok {
			return nil, ErrInvalidUUIDFormat
		} else {
			uuid[i] = v
		}
	}
	return uuid[:], nil
}

// xvalues returns the value of a byte as a hexadecimal digit or 255.
var xvalues = [256]byte{
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 255, 255, 255, 255, 255, 255,
	255, 10, 11, 12, 13, 14, 15, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 10, 11, 12, 13, 14, 15, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
}

// xtob converts hex characters x1 and x2 into a byte.
func xtob(x1, x2 byte) (byte, bool) {
	b1 := xvalues[x1]
	b2 := xvalues[x2]
	return (b1 << 4) | b2, b1 != 255 && b2 != 255
}
