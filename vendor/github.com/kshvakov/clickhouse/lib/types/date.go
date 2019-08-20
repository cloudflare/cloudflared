// Timezoneless date/datetime types

package types

import (
	"database/sql/driver"
	"time"
)

// Truncate timezone
//
//   clickhouse.Date(time.Date(2017, 1, 1, 0, 0, 0, 0, time.Local)) -> time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
type Date time.Time

func (date Date) Value() (driver.Value, error) {
	return date.convert(), nil
}

func (date Date) convert() time.Time {
	return time.Date(time.Time(date).Year(), time.Time(date).Month(), time.Time(date).Day(), 0, 0, 0, 0, time.UTC)
}

// Truncate timezone
//
//   clickhouse.DateTime(time.Date(2017, 1, 1, 0, 0, 0, 0, time.Local)) -> time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
type DateTime time.Time

func (datetime DateTime) Value() (driver.Value, error) {
	return datetime.convert(), nil
}

func (datetime DateTime) convert() time.Time {
	return time.Date(
		time.Time(datetime).Year(),
		time.Time(datetime).Month(),
		time.Time(datetime).Day(),
		time.Time(datetime).Hour(),
		time.Time(datetime).Minute(),
		time.Time(datetime).Second(),
		1,
		time.UTC,
	)
}

var (
	_ driver.Valuer = Date{}
	_ driver.Valuer = DateTime{}
)
