package tunnelrpc

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/google/uuid"
	"zombiezen.com/go/capnproto2/rpc"

	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

type RegistrationClient interface {
	RegisterConnection(
		ctx context.Context,
		auth pogs.TunnelAuth,
		tunnelID uuid.UUID,
		options *pogs.ConnectionOptions,
		connIndex uint8,
		edgeAddress net.IP,
	) (*pogs.ConnectionDetails, error)
	SendLocalConfiguration(ctx context.Context, config []byte) error
	GracefulShutdown(ctx context.Context, gracePeriod time.Duration)
	Close()
}

type registrationClient struct {
	client         pogs.RegistrationServer_PogsClient
	transport      rpc.Transport
	requestTimeout time.Duration
}

func NewRegistrationClient(ctx context.Context, stream io.ReadWriteCloser, requestTimeout time.Duration) RegistrationClient {
	transport := SafeTransport(stream)
	conn := rpc.NewConn(transport)
	client := pogs.NewRegistrationServer_PogsClient(conn.Bootstrap(ctx), conn)
	return &registrationClient{
		client:         client,
		transport:      transport,
		requestTimeout: requestTimeout,
	}
}

func (r *registrationClient) RegisterConnection(
	ctx context.Context,
	auth pogs.TunnelAuth,
	tunnelID uuid.UUID,
	options *pogs.ConnectionOptions,
	connIndex uint8,
	edgeAddress net.IP,
) (*pogs.ConnectionDetails, error) {
	ctx, cancel := context.WithTimeout(ctx, r.requestTimeout)
	defer cancel()
	return r.client.RegisterConnection(ctx, auth, tunnelID, connIndex, options)
}

func (r *registrationClient) SendLocalConfiguration(ctx context.Context, config []byte) error {
	ctx, cancel := context.WithTimeout(ctx, r.requestTimeout)
	defer cancel()
	return r.client.SendLocalConfiguration(ctx, config)
}

func (r *registrationClient) GracefulShutdown(ctx context.Context, gracePeriod time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, gracePeriod)
	defer cancel()
	_ = r.client.UnregisterConnection(ctx)
}

func (r *registrationClient) Close() {
	// Closing the client will also close the connection
	_ = r.client.Close()
	// Closing the transport also closes the stream
	_ = r.transport.Close()
}
