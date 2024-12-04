package diagnostic

import (
	"errors"
)

var (
	// Error used when there is no log directory available.
	ErrManagedLogNotFound = errors.New("managed log directory not found")
	// Error used when it is not possible to collect logs using the log configuration.
	ErrLogConfigurationIsInvalid = errors.New("provided log configuration is invalid")
	// Error used when parsing the fields of the output of collector.
	ErrInsufficientLines = errors.New("insufficient lines")
	// Error used when parsing the lines of the output of collector.
	ErrInsuficientFields = errors.New("insufficient fields")
	// Error used when given key is not found while parsing KV.
	ErrKeyNotFound = errors.New("key not found")
	// Error used when there is no disk volume information available.
	ErrNoVolumeFound = errors.New("no disk volume information found")
	// Error user when the base url of the diagnostic client is not provided.
	ErrNoBaseURL = errors.New("no base url")
	// Error used when no metrics server is found listening to the known addresses list (check [metrics.GetMetricsKnownAddresses]).
	ErrMetricsServerNotFound = errors.New("metrics server not found")
	// Error used when multiple metrics server are found listening to the known addresses list (check [metrics.GetMetricsKnownAddresses]).
	ErrMultipleMetricsServerFound = errors.New("multiple metrics server found")
	// Error used when a temporary file creation fails within the diagnostic procedure
	ErrCreatingTemporaryFile = errors.New("temporary file creation failed")
)
