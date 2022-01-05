package connection

import (
	"context"
	"io"
	"time"

	"github.com/rs/zerolog"

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// RPCClientFunc derives a named tunnel rpc client that can then be used to register and unregister connections.
type RPCClientFunc func(context.Context, io.ReadWriteCloser, *zerolog.Logger) NamedTunnelRPCClient

type controlStream struct {
	observer *Observer

	connectedFuse     ConnectedFuse
	namedTunnelConfig *NamedTunnelConfig
	connIndex         uint8

	newRPCClientFunc RPCClientFunc

	gracefulShutdownC <-chan struct{}
	gracePeriod       time.Duration
	stoppedGracefully bool
}

// ControlStreamHandler registers connections with origintunneld and initiates graceful shutdown.
type ControlStreamHandler interface {
	// ServeControlStream handles the control plane of the transport in the current goroutine calling this
	ServeControlStream(ctx context.Context, rw io.ReadWriteCloser, connOptions *tunnelpogs.ConnectionOptions) error
	// IsStopped tells whether the method above has finished
	IsStopped() bool
}

// NewControlStream returns a new instance of ControlStreamHandler
func NewControlStream(
	observer *Observer,
	connectedFuse ConnectedFuse,
	namedTunnelConfig *NamedTunnelConfig,
	connIndex uint8,
	newRPCClientFunc RPCClientFunc,
	gracefulShutdownC <-chan struct{},
	gracePeriod time.Duration,
) ControlStreamHandler {
	if newRPCClientFunc == nil {
		newRPCClientFunc = newRegistrationRPCClient
	}
	return &controlStream{
		observer:          observer,
		connectedFuse:     connectedFuse,
		namedTunnelConfig: namedTunnelConfig,
		newRPCClientFunc:  newRPCClientFunc,
		connIndex:         connIndex,
		gracefulShutdownC: gracefulShutdownC,
		gracePeriod:       gracePeriod,
	}
}

func (c *controlStream) ServeControlStream(
	ctx context.Context,
	rw io.ReadWriteCloser,
	connOptions *tunnelpogs.ConnectionOptions,
) error {
	rpcClient := c.newRPCClientFunc(ctx, rw, c.observer.log)

	if err := rpcClient.RegisterConnection(ctx, c.namedTunnelConfig, connOptions, c.connIndex, c.observer); err != nil {
		rpcClient.Close()
		return err
	}
	c.connectedFuse.Connected()

	c.waitForUnregister(ctx, rpcClient)
	return nil
}

func (c *controlStream) waitForUnregister(ctx context.Context, rpcClient NamedTunnelRPCClient) {
	// wait for connection termination or start of graceful shutdown
	defer rpcClient.Close()
	select {
	case <-ctx.Done():
		break
	case <-c.gracefulShutdownC:
		c.stoppedGracefully = true
	}

	c.observer.sendUnregisteringEvent(c.connIndex)
	rpcClient.GracefulShutdown(ctx, c.gracePeriod)
	c.observer.log.Info().Uint8(LogFieldConnIndex, c.connIndex).Msg("Unregistered tunnel connection")
}

func (c *controlStream) IsStopped() bool {
	return c.stoppedGracefully
}
