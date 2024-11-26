package diagnostic

import (
	"errors"
)

var (
	// Error used when there is no log directory available.
	ErrManagedLogNotFound = errors.New("managed log directory not found")
	// Error used when one key is not found.
	ErrMustNotBeEmpty = errors.New("provided argument is empty")
	// Error used when parsing the fields of the output of collector.
	ErrInsufficientLines = errors.New("insufficient lines")
	// Error used when parsing the lines of the output of collector.
	ErrInsuficientFields = errors.New("insufficient fields")
	// Error used when given key is not found while parsing KV.
	ErrKeyNotFound = errors.New("key not found")
	// Error used when there is no disk volume information available.
	ErrNoVolumeFound   = errors.New("no disk volume information found")
	ErrNoPathAvailable = errors.New("no path available")
)
