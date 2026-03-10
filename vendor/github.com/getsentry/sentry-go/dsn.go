package sentry

import (
	"github.com/getsentry/sentry-go/internal/protocol"
)

// Re-export protocol types to maintain public API compatibility

// Dsn is used as the remote address source to client transport.
type Dsn struct {
	protocol.Dsn
}

// DsnParseError represents an error that occurs if a Sentry
// DSN cannot be parsed.
type DsnParseError = protocol.DsnParseError

// NewDsn creates a Dsn by parsing rawURL. Most users will never call this
// function directly. It is provided for use in custom Transport
// implementations.
func NewDsn(rawURL string) (*Dsn, error) {
	protocolDsn, err := protocol.NewDsn(rawURL)
	if err != nil {
		return nil, err
	}
	return &Dsn{Dsn: *protocolDsn}, nil
}

// RequestHeaders returns all the necessary headers that have to be used in the transport when sending events
// to the /store endpoint.
//
// Deprecated: This method shall only be used if you want to implement your own transport that sends events to
// the /store endpoint. If you're using the transport provided by the SDK, all necessary headers to authenticate
// against the /envelope endpoint are added automatically.
func (dsn Dsn) RequestHeaders() map[string]string {
	return dsn.Dsn.RequestHeaders(SDKVersion)
}
