package column

import (
	"time"

	"github.com/kshvakov/clickhouse/lib/binary"
)

type Date struct {
	base
	Timezone *time.Location
	offset   int64
}

func (dt *Date) Read(decoder *binary.Decoder) (interface{}, error) {
	sec, err := decoder.Int16()
	if err != nil {
		return nil, err
	}
	return time.Unix(int64(sec)*24*3600-dt.offset, 0).In(dt.Timezone), nil
}

func (dt *Date) Write(encoder *binary.Encoder, v interface{}) error {
	var timestamp int64
	switch value := v.(type) {
	case time.Time:
		_, offset := value.Zone()
		timestamp = value.Unix() + int64(offset)
	case int16:
		return encoder.Int16(value)
	case int32:
		timestamp = int64(value) + dt.offset
	case int64:
		timestamp = value + dt.offset
	case string:
		var err error
		timestamp, err = dt.parse(value)
		if err != nil {
			return err
		}

	// this relies on Nullable never sending nil values through
	case *time.Time:
		_, offset := value.Zone()
		timestamp = (*value).Unix() + int64(offset)
	case *int16:
		return encoder.Int16(*value)
	case *int32:
		timestamp = int64(*value) + dt.offset
	case *int64:
		timestamp = *value + dt.offset
	case *string:
		var err error
		timestamp, err = dt.parse(*value)
		if err != nil {
			return err
		}

	default:
		return &ErrUnexpectedType{
			T:      v,
			Column: dt,
		}
	}

	return encoder.Int16(int16(timestamp / 24 / 3600))
}

func (dt *Date) parse(value string) (int64, error) {
	tv, err := time.Parse("2006-01-02", value)
	if err != nil {
		return 0, err
	}
	return time.Date(
		time.Time(tv).Year(),
		time.Time(tv).Month(),
		time.Time(tv).Day(),
		0, 0, 0, 0, time.UTC,
	).Unix(), nil
}
