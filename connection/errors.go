package connection

import (
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/h2mux"
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

type muxerShutdownError struct{}

func (e muxerShutdownError) Error() string {
	return "muxer shutdown"
}

var errMuxerStopped = muxerShutdownError{}

func isHandshakeErrRecoverable(err error, connIndex uint8, observer *Observer) bool {
	log := observer.log.With().
		Uint8(LogFieldConnIndex, connIndex).
		Err(err).
		Logger()

	switch err.(type) {
	case edgediscovery.DialError:
		log.Error().Msg("Connection unable to dial edge")
	case h2mux.MuxerHandshakeError:
		log.Error().Msg("Connection handshake with edge server failed")
	default:
		log.Error().Msg("Connection failed")
		return false
	}
	return true
}
