// +build go1.16

package qerr

import (
	"net"
)

func (e *TransportError) Is(target error) bool {
	_, ok := target.(*TransportError)
	if ok {
		return true
	}
	return target == net.ErrClosed
}

func (e *ApplicationError) Is(target error) bool {
	_, ok := target.(*ApplicationError)
	if ok {
		return true
	}
	return target == net.ErrClosed
}

func (e *IdleTimeoutError) Is(target error) bool {
	_, ok := target.(*IdleTimeoutError)
	if ok {
		return true
	}
	return target == net.ErrClosed
}

func (e *HandshakeTimeoutError) Is(target error) bool {
	_, ok := target.(*HandshakeTimeoutError)
	if ok {
		return true
	}
	return target == net.ErrClosed
}

func (e *VersionNegotiationError) Is(target error) bool {
	_, ok := target.(*VersionNegotiationError)
	if ok {
		return true
	}
	return target == net.ErrClosed
}

func (e *StatelessResetError) Is(target error) bool {
	_, ok := target.(*StatelessResetError)
	if ok {
		return true
	}
	return target == net.ErrClosed
}
