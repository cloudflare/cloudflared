package ws

// RejectOption represents an option used to control the way connection is
// rejected.
type RejectOption func(*ConnectionRejectedError)

// RejectionReason returns an option that makes connection to be rejected with
// given reason.
func RejectionReason(reason string) RejectOption {
	return func(err *ConnectionRejectedError) {
		err.reason = reason
	}
}

// RejectionStatus returns an option that makes connection to be rejected with
// given HTTP status code.
func RejectionStatus(code int) RejectOption {
	return func(err *ConnectionRejectedError) {
		err.code = code
	}
}

// RejectionHeader returns an option that makes connection to be rejected with
// given HTTP headers.
func RejectionHeader(h HandshakeHeader) RejectOption {
	return func(err *ConnectionRejectedError) {
		err.header = h
	}
}

// RejectConnectionError constructs an error that could be used to control the
// way handshake is rejected by Upgrader.
func RejectConnectionError(options ...RejectOption) error {
	err := new(ConnectionRejectedError)
	for _, opt := range options {
		opt(err)
	}
	return err
}

// ConnectionRejectedError represents a rejection of connection during
// WebSocket handshake error.
//
// It can be returned by Upgrader's On* hooks to indicate that WebSocket
// handshake should be rejected.
type ConnectionRejectedError struct {
	reason string
	code   int
	header HandshakeHeader
}

// Error implements error interface.
func (r *ConnectionRejectedError) Error() string {
	return r.reason
}

func (r *ConnectionRejectedError) StatusCode() int {
	return r.code
}
