package quic

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"zombiezen.com/go/capnproto2/rpc"

	"github.com/google/uuid"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/cloudflare/cloudflared/tunnelrpc/metrics"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// CloudflaredClient calls capnp rpc methods of SessionManager and ConfigurationManager.
type CloudflaredClient struct {
	client         pogs.CloudflaredServer_PogsClient
	transport      rpc.Transport
	requestTimeout time.Duration
}

func NewCloudflaredClient(ctx context.Context, stream io.ReadWriteCloser, requestTimeout time.Duration) (*CloudflaredClient, error) {
	n, err := stream.Write(rpcStreamProtocolSignature[:])
	if err != nil {
		return nil, err
	}
	if n != len(rpcStreamProtocolSignature) {
		return nil, fmt.Errorf("expect to write %d bytes for RPC stream protocol signature, wrote %d", len(rpcStreamProtocolSignature), n)
	}
	transport := tunnelrpc.SafeTransport(stream)
	conn := tunnelrpc.NewClientConn(transport)
	client := pogs.NewCloudflaredServer_PogsClient(conn.Bootstrap(ctx), conn)
	return &CloudflaredClient{
		client:         client,
		transport:      transport,
		requestTimeout: requestTimeout,
	}, nil
}

func (c *CloudflaredClient) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeIdleAfterHint time.Duration, traceContext string) (*pogs.RegisterUdpSessionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	defer metrics.CapnpMetrics.ClientOperations.WithLabelValues(metrics.Cloudflared, metrics.OperationRegisterUdpSession).Inc()
	timer := metrics.NewClientOperationLatencyObserver(metrics.Cloudflared, metrics.OperationRegisterUdpSession)
	defer timer.ObserveDuration()

	resp, err := c.client.RegisterUdpSession(ctx, sessionID, dstIP, dstPort, closeIdleAfterHint, traceContext)
	if err != nil {
		metrics.CapnpMetrics.ClientFailures.WithLabelValues(metrics.Cloudflared, metrics.OperationRegisterUdpSession).Inc()
	}
	return resp, err
}

func (c *CloudflaredClient) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	defer metrics.CapnpMetrics.ClientOperations.WithLabelValues(metrics.Cloudflared, metrics.OperationUnregisterUdpSession).Inc()
	timer := metrics.NewClientOperationLatencyObserver(metrics.Cloudflared, metrics.OperationUnregisterUdpSession)
	defer timer.ObserveDuration()

	err := c.client.UnregisterUdpSession(ctx, sessionID, message)
	if err != nil {
		metrics.CapnpMetrics.ClientFailures.WithLabelValues(metrics.Cloudflared, metrics.OperationUnregisterUdpSession).Inc()
	}
	return err
}

func (c *CloudflaredClient) UpdateConfiguration(ctx context.Context, version int32, config []byte) (*pogs.UpdateConfigurationResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	defer metrics.CapnpMetrics.ClientOperations.WithLabelValues(metrics.Cloudflared, metrics.OperationUpdateConfiguration).Inc()
	timer := metrics.NewClientOperationLatencyObserver(metrics.Cloudflared, metrics.OperationUpdateConfiguration)
	defer timer.ObserveDuration()

	resp, err := c.client.UpdateConfiguration(ctx, version, config)
	if err != nil {
		metrics.CapnpMetrics.ClientFailures.WithLabelValues(metrics.Cloudflared, metrics.OperationUpdateConfiguration).Inc()
	}
	return resp, err
}

func (c *CloudflaredClient) Close() {
	_ = c.client.Close()
	_ = c.transport.Close()
}
