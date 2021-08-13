package logging

// A CloseReason is the reason why a QUIC connection is closed.
// It falls in one of 4 categories:
// 1. The application closed the connection (with an application-specific error code).
// 2. The transport closed the connection with a transport-error code.
// 3. The connection timed out, either during the handshake, or due to an idle timeout.
// 4. A stateless reset was received.
type CloseReason struct {
	remote           bool
	applicationError *ApplicationError
	transportError   *TransportError

	timeout             *TimeoutReason
	statelessResetToken *StatelessResetToken
	versions            []VersionNumber
}

// NewApplicationCloseReason creates a new CloseReason for an application error.
func NewApplicationCloseReason(errorCode ApplicationError, remote bool) CloseReason {
	return CloseReason{remote: remote, applicationError: &errorCode}
}

// NewTransportCloseReason creates a new CloseReason for a transport error.
func NewTransportCloseReason(errorCode TransportError, remote bool) CloseReason {
	return CloseReason{remote: remote, transportError: &errorCode}
}

// NewTimeoutCloseReason creates a new CloseReason for a connection timeout.
func NewTimeoutCloseReason(r TimeoutReason) CloseReason {
	return CloseReason{timeout: &r}
}

// NewStatelessResetCloseReason creates a new CloseReason for a stateless reset.
func NewStatelessResetCloseReason(token StatelessResetToken) CloseReason {
	return CloseReason{statelessResetToken: &token}
}

// NewVersionNegotiationError creates a new CloseReason for a version negotiation error.
func NewVersionNegotiationError(versions []VersionNumber) CloseReason {
	return CloseReason{versions: versions}
}

// ApplicationError gets the application error.
func (r *CloseReason) ApplicationError() (errorCode ApplicationError, remote bool, ok bool) {
	if r.applicationError == nil {
		return
	}
	return *r.applicationError, r.remote, true
}

// TransportError gets the transport error.
func (r *CloseReason) TransportError() (errorCode TransportError, remote bool, ok bool) {
	if r.transportError == nil {
		return
	}
	return *r.transportError, r.remote, true
}

// Timeout gets the timeout error.
func (r *CloseReason) Timeout() (reason TimeoutReason, ok bool) {
	if r.timeout == nil {
		return
	}
	return *r.timeout, true
}

// StatelessReset gets the stateless reset token.
func (r *CloseReason) StatelessReset() (token StatelessResetToken, ok bool) {
	if r.statelessResetToken == nil {
		return
	}
	return *r.statelessResetToken, true
}

func (r *CloseReason) VersionNegotiation() (versions []VersionNumber, ok bool) {
	return r.versions, len(r.versions) > 0
}
