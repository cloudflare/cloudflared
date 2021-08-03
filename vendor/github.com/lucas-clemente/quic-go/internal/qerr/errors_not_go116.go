// +build !go1.16

package qerr

func (e *TransportError) Is(target error) bool {
	_, ok := target.(*TransportError)
	return ok
}

func (e *ApplicationError) Is(target error) bool {
	_, ok := target.(*ApplicationError)
	return ok
}

func (e *IdleTimeoutError) Is(target error) bool {
	_, ok := target.(*IdleTimeoutError)
	return ok
}

func (e *HandshakeTimeoutError) Is(target error) bool {
	_, ok := target.(*HandshakeTimeoutError)
	return ok
}

func (e *VersionNegotiationError) Is(target error) bool {
	_, ok := target.(*VersionNegotiationError)
	return ok
}

func (e *StatelessResetError) Is(target error) bool {
	_, ok := target.(*StatelessResetError)
	return ok
}
