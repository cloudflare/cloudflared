package quic

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/google/uuid"
	"zombiezen.com/go/capnproto2/rpc"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/cloudflare/cloudflared/tunnelrpc/metrics"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// SessionClient calls capnp rpc methods of SessionManager.
type SessionClient struct {
	client         pogs.SessionManager_PogsClient
	transport      rpc.Transport
	requestTimeout time.Duration
}

func NewSessionClient(ctx context.Context, stream io.ReadWriteCloser, requestTimeout time.Duration) (*SessionClient, error) {
	n, err := stream.Write(rpcStreamProtocolSignature[:])
	if err != nil {
		return nil, err
	}
	if n != len(rpcStreamProtocolSignature) {
		return nil, fmt.Errorf("expect to write %d bytes for RPC stream protocol signature, wrote %d", len(rpcStreamProtocolSignature), n)
	}
	transport := tunnelrpc.SafeTransport(stream)
	conn := tunnelrpc.NewClientConn(transport)
	return &SessionClient{
		client:         pogs.NewSessionManager_PogsClient(conn.Bootstrap(ctx), conn),
		transport:      transport,
		requestTimeout: requestTimeout,
	}, nil
}

func (c *SessionClient) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeIdleAfterHint time.Duration, traceContext string) (*pogs.RegisterUdpSessionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	defer metrics.CapnpMetrics.ClientOperations.WithLabelValues(metrics.SessionManager, metrics.OperationRegisterUdpSession).Inc()
	timer := metrics.NewClientOperationLatencyObserver(metrics.SessionManager, metrics.OperationRegisterUdpSession)
	defer timer.ObserveDuration()

	resp, err := c.client.RegisterUdpSession(ctx, sessionID, dstIP, dstPort, closeIdleAfterHint, traceContext)
	if err != nil {
		metrics.CapnpMetrics.ClientFailures.WithLabelValues(metrics.SessionManager, metrics.OperationRegisterUdpSession).Inc()
	}
	return resp, err
}

func (c *SessionClient) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	defer metrics.CapnpMetrics.ClientOperations.WithLabelValues(metrics.SessionManager, metrics.OperationUnregisterUdpSession).Inc()
	timer := metrics.NewClientOperationLatencyObserver(metrics.SessionManager, metrics.OperationUnregisterUdpSession)
	defer timer.ObserveDuration()

	err := c.client.UnregisterUdpSession(ctx, sessionID, message)
	if err != nil {
		metrics.CapnpMetrics.ClientFailures.WithLabelValues(metrics.SessionManager, metrics.OperationUnregisterUdpSession).Inc()
	}
	return err
}

func (c *SessionClient) Close() {
	_ = c.client.Close()
	_ = c.transport.Close()
}
