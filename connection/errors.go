package connection

import (
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/h2mux"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	DuplicateConnectionError = "EDUPCONN"
)

// RegisterTunnel error from client
type clientRegisterTunnelError struct {
	cause error
}

func newRPCError(cause error, counter *prometheus.CounterVec, name rpcName) clientRegisterTunnelError {
	counter.WithLabelValues(cause.Error(), string(name)).Inc()
	return clientRegisterTunnelError{cause: cause}
}

func (e clientRegisterTunnelError) Error() string {
	return e.cause.Error()
}

type DupConnRegisterTunnelError struct{}

var errDuplicationConnection = &DupConnRegisterTunnelError{}

func (e DupConnRegisterTunnelError) Error() string {
	return "already connected to this server, trying another address"
}

// RegisterTunnel error from server
type serverRegisterTunnelError struct {
	cause     error
	permanent bool
}

func (e serverRegisterTunnelError) Error() string {
	return e.cause.Error()
}

func serverRegistrationErrorFromRPC(err error) *serverRegisterTunnelError {
	if retryable, ok := err.(*tunnelpogs.RetryableError); ok {
		return &serverRegisterTunnelError{
			cause:     retryable.Unwrap(),
			permanent: false,
		}
	}
	return &serverRegisterTunnelError{
		cause:     err,
		permanent: true,
	}
}

type muxerShutdownError struct{}

func (e muxerShutdownError) Error() string {
	return "muxer shutdown"
}

func isHandshakeErrRecoverable(err error, connIndex uint8, observer *Observer) bool {
	switch err.(type) {
	case edgediscovery.DialError:
		observer.Errorf("Connection %d unable to dial edge: %s", connIndex, err)
	case h2mux.MuxerHandshakeError:
		observer.Errorf("Connection %d handshake with edge server failed: %s", connIndex, err)
	default:
		observer.Errorf("Connection %d failed: %s", connIndex, err)
		return false
	}
	return true
}
