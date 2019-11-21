package connection

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/cloudflare/cloudflared/h2mux"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	openStreamTimeout = 30 * time.Second
)

type Connection struct {
	id    uuid.UUID
	muxer *h2mux.Muxer
}

func newConnection(muxer *h2mux.Muxer) (*Connection, error) {
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
func (c *Connection) Connect(ctx context.Context, parameters *tunnelpogs.ConnectParameters, logger *logrus.Entry) (tunnelpogs.ConnectResult, error) {
	tsClient, err := NewRPCClient(ctx, c.muxer, logger.WithField("rpc", "connect"), openStreamTimeout)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create new RPC connection")
	}
	defer tsClient.Close()
	return tsClient.Connect(ctx, parameters)
}

func (c *Connection) Shutdown() {
	c.muxer.Shutdown()
}
