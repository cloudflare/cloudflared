package diagnostic

import (
	"errors"
)

var (
	// Error used when parsing the fields of the output of collector.
	ErrInsufficientLines = errors.New("insufficient lines")
	// Error used when parsing the lines of the output of collector.
	ErrInsuficientFields = errors.New("insufficient fields")
	// Error used when given key is not found while parsing KV.
	ErrKeyNotFound = errors.New("key not found")
	// Error used when tehre is no disk volume information available
	ErrNoVolumeFound = errors.New("No disk volume information found")
)
