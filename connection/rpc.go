package connection

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/rs/zerolog"
	"zombiezen.com/go/capnproto2/rpc"

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

type NamedTunnelRPCClient interface {
	RegisterConnection(
		c context.Context,
		config *NamedTunnelProperties,
		options *tunnelpogs.ConnectionOptions,
		connIndex uint8,
		edgeAddress net.IP,
		observer *Observer,
	) (*tunnelpogs.ConnectionDetails, error)
	SendLocalConfiguration(
		c context.Context,
		config []byte,
		observer *Observer,
	) error
	GracefulShutdown(ctx context.Context, gracePeriod time.Duration)
	Close()
}

type registrationServerClient struct {
	client    tunnelpogs.RegistrationServer_PogsClient
	transport rpc.Transport
}

func newRegistrationRPCClient(
	ctx context.Context,
	stream io.ReadWriteCloser,
	log *zerolog.Logger,
) NamedTunnelRPCClient {
	transport := rpc.StreamTransport(stream)
	conn := rpc.NewConn(transport)
	return &registrationServerClient{
		client:    tunnelpogs.RegistrationServer_PogsClient{Client: conn.Bootstrap(ctx), Conn: conn},
		transport: transport,
	}
}

func (rsc *registrationServerClient) RegisterConnection(
	ctx context.Context,
	properties *NamedTunnelProperties,
	options *tunnelpogs.ConnectionOptions,
	connIndex uint8,
	edgeAddress net.IP,
	observer *Observer,
) (*tunnelpogs.ConnectionDetails, error) {
	conn, err := rsc.client.RegisterConnection(
		ctx,
		properties.Credentials.Auth(),
		properties.Credentials.TunnelID,
		connIndex,
		options,
	)
	if err != nil {
		if err.Error() == DuplicateConnectionError {
			observer.metrics.regFail.WithLabelValues("dup_edge_conn", "registerConnection").Inc()
			return nil, errDuplicationConnection
		}
		observer.metrics.regFail.WithLabelValues("server_error", "registerConnection").Inc()
		return nil, serverRegistrationErrorFromRPC(err)
	}

	observer.metrics.regSuccess.WithLabelValues("registerConnection").Inc()

	return conn, nil
}

func (rsc *registrationServerClient) SendLocalConfiguration(ctx context.Context, config []byte, observer *Observer) (err error) {
	observer.metrics.localConfigMetrics.pushes.Inc()
	defer func() {
		if err != nil {
			observer.metrics.localConfigMetrics.pushesErrors.Inc()
		}
	}()

	return rsc.client.SendLocalConfiguration(ctx, config)
}

func (rsc *registrationServerClient) GracefulShutdown(ctx context.Context, gracePeriod time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, gracePeriod)
	defer cancel()
	_ = rsc.client.UnregisterConnection(ctx)
}

func (rsc *registrationServerClient) Close() {
	// Closing the client will also close the connection
	_ = rsc.client.Close()
	// Closing the transport also closes the stream
	_ = rsc.transport.Close()
}

type rpcName string

const (
	register     rpcName = "register"
	reconnect    rpcName = "reconnect"
	unregister   rpcName = "unregister"
	authenticate rpcName = " authenticate"
)
