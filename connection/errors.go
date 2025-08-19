package connection

import (
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	DuplicateConnectionError = "EDUPCONN"
)

type DupConnRegisterTunnelError struct{}

var errDuplicationConnection = DupConnRegisterTunnelError{}

func (e DupConnRegisterTunnelError) Error() string {
	return "already connected to this server, trying another address"
}

// Dial to edge server with quic failed
type EdgeQuicDialError struct {
	Cause error
}

func (e *EdgeQuicDialError) Error() string {
	return "failed to dial to edge with quic: " + e.Cause.Error()
}

func (e *EdgeQuicDialError) Unwrap() error {
	return e.Cause
}

// RegisterTunnel error from server
type ServerRegisterTunnelError struct {
	Cause     error
	Permanent bool
}

func (e ServerRegisterTunnelError) Error() string {
	return e.Cause.Error()
}

func serverRegistrationErrorFromRPC(err error) ServerRegisterTunnelError {
	if retryable, ok := err.(*tunnelpogs.RetryableError); ok {
		return ServerRegisterTunnelError{
			Cause:     retryable.Unwrap(),
			Permanent: false,
		}
	}
	return ServerRegisterTunnelError{
		Cause:     err,
		Permanent: true,
	}
}

type ControlStreamError struct{}

var _ error = &ControlStreamError{}

func (e *ControlStreamError) Error() string {
	return "control stream encountered a failure while serving"
}

type StreamListenerError struct{}

var _ error = &StreamListenerError{}

func (e *StreamListenerError) Error() string {
	return "accept stream listener encountered a failure while serving"
}

type DatagramManagerError struct{}

var _ error = &DatagramManagerError{}

func (e *DatagramManagerError) Error() string {
	return "datagram manager encountered a failure while serving"
}
