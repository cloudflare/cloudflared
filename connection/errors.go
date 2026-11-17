package connection

import (
	"errors"
	"fmt"

	"github.com/quic-go/quic-go"

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	DuplicateConnectionError = "EDUPCONN"
	// RegionChangeQUICCode tells the connector that the account region changed.
	RegionChangeQUICCode quic.ApplicationErrorCode = 10002
)

var ErrDuplicateConnection = errors.New("already connected to this server, trying another address")

// ErrRegionChanged requests a restart of the entire HA connection group.
var ErrRegionChanged = errors.New("account region changed")

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

func (e ServerRegisterTunnelError) Unwrap() error {
	return e.Cause
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

type ControlStreamError struct {
	Cause error
}

var _ error = &ControlStreamError{}

func (e *ControlStreamError) Error() string {
	return fmt.Sprintf("control stream error: %s", e.Cause)
}

func (e *ControlStreamError) Unwrap() error {
	return e.Cause
}

type StreamListenerError struct {
	Cause error
}

var _ error = &StreamListenerError{}

func (e *StreamListenerError) Error() string {
	return fmt.Sprintf("accept stream listener error: %s", e.Cause)
}

func (e *StreamListenerError) Unwrap() error {
	return e.Cause
}

type DatagramManagerError struct {
	Cause error
}

var _ error = &DatagramManagerError{}

func (e *DatagramManagerError) Error() string {
	return fmt.Sprintf("datagram manager error: %s", e.Cause)
}

func (e *DatagramManagerError) Unwrap() error {
	return e.Cause
}
