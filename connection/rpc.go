package connection

import (
	"context"
	"io"

	rpc "zombiezen.com/go/capnproto2/rpc"

	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// NewTunnelRPCClient creates and returns a new RPC client, which will communicate
// using a stream on the given muxer
func NewTunnelRPCClient(
	ctx context.Context,
	stream io.ReadWriteCloser,
	logger logger.Service,
) (client tunnelpogs.TunnelServer_PogsClient, err error) {
	conn := rpc.NewConn(
		tunnelrpc.NewTransportLogger(logger, rpc.StreamTransport(stream)),
		tunnelrpc.ConnLog(logger),
	)
	registrationClient := tunnelpogs.RegistrationServer_PogsClient{Client: conn.Bootstrap(ctx), Conn: conn}
	client = tunnelpogs.TunnelServer_PogsClient{RegistrationServer_PogsClient: registrationClient, Client: conn.Bootstrap(ctx), Conn: conn}
	return client, nil
}

func NewRegistrationRPCClient(
	ctx context.Context,
	stream io.ReadWriteCloser,
	logger logger.Service,
) (client tunnelpogs.RegistrationServer_PogsClient, err error) {
	conn := rpc.NewConn(
		tunnelrpc.NewTransportLogger(logger, rpc.StreamTransport(stream)),
		tunnelrpc.ConnLog(logger),
	)
	client = tunnelpogs.RegistrationServer_PogsClient{Client: conn.Bootstrap(ctx), Conn: conn}
	return client, nil
}
