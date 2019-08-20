package clickhouse

import (
	"time"
)

func Array(v interface{}) interface{} {
	return v
}

func ArrayFixedString(len int, v interface{}) interface{} {
	return v
}

func ArrayDate(v []time.Time) interface{} {
	return v
}

func ArrayDateTime(v []time.Time) interface{} {
	return v
}
