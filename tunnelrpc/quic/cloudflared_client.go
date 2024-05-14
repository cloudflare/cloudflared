package quic

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"zombiezen.com/go/capnproto2/rpc"

	"github.com/google/uuid"

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
	transport := rpc.StreamTransport(stream)
	conn := rpc.NewConn(transport)
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
	return c.client.RegisterUdpSession(ctx, sessionID, dstIP, dstPort, closeIdleAfterHint, traceContext)
}

func (c *CloudflaredClient) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, message string) error {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	return c.client.UnregisterUdpSession(ctx, sessionID, message)
}

func (c *CloudflaredClient) UpdateConfiguration(ctx context.Context, version int32, config []byte) (*pogs.UpdateConfigurationResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	return c.client.UpdateConfiguration(ctx, version, config)
}

func (c *CloudflaredClient) Close() {
	_ = c.client.Close()
	_ = c.transport.Close()
}
