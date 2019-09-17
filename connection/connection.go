package connection

import (
	"context"
	"net"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	rpc "zombiezen.com/go/capnproto2/rpc"
)

const (
	openStreamTimeout = 30 * time.Second
)

type dialError struct {
	cause error
}

func (e dialError) Error() string {
	return e.cause.Error()
}

type Connection struct {
	id    uuid.UUID
	muxer *h2mux.Muxer
}

func newConnection(muxer *h2mux.Muxer, edgeIP *net.TCPAddr) (*Connection, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	return &Connection{
		id:    id,
		muxer: muxer,
	}, nil
}

func (c *Connection) Serve(ctx context.Context) error {
	// Serve doesn't return until h2mux is shutdown
	return c.muxer.Serve(ctx)
}

// Connect is used to establish connections with cloudflare's edge network
func (c *Connection) Connect(ctx context.Context, parameters *tunnelpogs.ConnectParameters, logger *logrus.Entry) (pogs.ConnectResult, error) {
	openStreamCtx, cancel := context.WithTimeout(ctx, openStreamTimeout)
	defer cancel()

	rpcConn, err := c.newRPConn(openStreamCtx, logger)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create new RPC connection")
	}
	defer rpcConn.Close()

	tsClient := tunnelpogs.TunnelServer_PogsClient{Client: rpcConn.Bootstrap(ctx)}

	return tsClient.Connect(ctx, parameters)
}

func (c *Connection) Shutdown() {
	c.muxer.Shutdown()
}

func (c *Connection) newRPConn(ctx context.Context, logger *logrus.Entry) (*rpc.Conn, error) {
	stream, err := c.muxer.OpenRPCStream(ctx)
	if err != nil {
		return nil, err
	}
	return rpc.NewConn(
		tunnelrpc.NewTransportLogger(logger.WithField("rpc", "connect"), rpc.StreamTransport(stream)),
		tunnelrpc.ConnLog(logger.WithField("rpc", "connect")),
	), nil
}
