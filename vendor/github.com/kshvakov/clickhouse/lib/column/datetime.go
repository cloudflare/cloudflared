package column

import (
	"time"

	"github.com/kshvakov/clickhouse/lib/binary"
)

type DateTime struct {
	base
	Timezone *time.Location
}

func (dt *DateTime) Read(decoder *binary.Decoder) (interface{}, error) {
	sec, err := decoder.Int32()
	if err != nil {
		return nil, err
	}
	return time.Unix(int64(sec), 0).In(dt.Timezone), nil
}

func (dt *DateTime) Write(encoder *binary.Encoder, v interface{}) error {
	var timestamp int64
	switch value := v.(type) {
	case time.Time:
		if !value.IsZero() {
			timestamp = value.Unix()
		}
	case int16:
		timestamp = int64(value)
	case int32:
		timestamp = int64(value)
	case int64:
		timestamp = value
	case string:
		var err error
		timestamp, err = dt.parse(value)
		if err != nil {
			return err
		}

	case *time.Time:
		if value != nil && !(*value).IsZero() {
			timestamp = (*value).Unix()
		}
	case *int16:
		timestamp = int64(*value)
	case *int32:
		timestamp = int64(*value)
	case *int64:
		timestamp = *value
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

	return encoder.Int32(int32(timestamp))
}

func (dt *DateTime) parse(value string) (int64, error) {
	tv, err := time.Parse("2006-01-02 15:04:05", value)
	if err != nil {
		return 0, err
	}
	return time.Date(
		time.Time(tv).Year(),
		time.Time(tv).Month(),
		time.Time(tv).Day(),
		time.Time(tv).Hour(),
		time.Time(tv).Minute(),
		time.Time(tv).Second(),
		0, time.UTC,
	).Unix(), nil
}
