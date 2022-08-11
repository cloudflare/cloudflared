package connection

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/rs/zerolog"

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// RPCClientFunc derives a named tunnel rpc client that can then be used to register and unregister connections.
type RPCClientFunc func(context.Context, io.ReadWriteCloser, *zerolog.Logger) NamedTunnelRPCClient

type controlStream struct {
	observer *Observer

	connectedFuse         ConnectedFuse
	namedTunnelProperties *NamedTunnelProperties
	connIndex             uint8
	edgeAddress           net.IP

	newRPCClientFunc RPCClientFunc

	gracefulShutdownC <-chan struct{}
	gracePeriod       time.Duration
	stoppedGracefully bool
}

// ControlStreamHandler registers connections with origintunneld and initiates graceful shutdown.
type ControlStreamHandler interface {
	// ServeControlStream handles the control plane of the transport in the current goroutine calling this
	ServeControlStream(ctx context.Context, rw io.ReadWriteCloser, connOptions *tunnelpogs.ConnectionOptions, tunnelConfigGetter TunnelConfigJSONGetter) error
	// IsStopped tells whether the method above has finished
	IsStopped() bool
}

type TunnelConfigJSONGetter interface {
	GetConfigJSON() ([]byte, error)
}

// NewControlStream returns a new instance of ControlStreamHandler
func NewControlStream(
	observer *Observer,
	connectedFuse ConnectedFuse,
	namedTunnelConfig *NamedTunnelProperties,
	connIndex uint8,
	edgeAddress net.IP,
	newRPCClientFunc RPCClientFunc,
	gracefulShutdownC <-chan struct{},
	gracePeriod time.Duration,
) ControlStreamHandler {
	if newRPCClientFunc == nil {
		newRPCClientFunc = newRegistrationRPCClient
	}
	return &controlStream{
		observer:              observer,
		connectedFuse:         connectedFuse,
		namedTunnelProperties: namedTunnelConfig,
		newRPCClientFunc:      newRPCClientFunc,
		connIndex:             connIndex,
		edgeAddress:           edgeAddress,
		gracefulShutdownC:     gracefulShutdownC,
		gracePeriod:           gracePeriod,
	}
}

func (c *controlStream) ServeControlStream(
	ctx context.Context,
	rw io.ReadWriteCloser,
	connOptions *tunnelpogs.ConnectionOptions,
	tunnelConfigGetter TunnelConfigJSONGetter,
) error {
	rpcClient := c.newRPCClientFunc(ctx, rw, c.observer.log)

	registrationDetails, err := rpcClient.RegisterConnection(ctx, c.namedTunnelProperties, connOptions, c.connIndex, c.edgeAddress, c.observer)
	if err != nil {
		rpcClient.Close()
		return err
	}
	c.connectedFuse.Connected()

	// if conn index is 0 and tunnel is not remotely managed, then send local ingress rules configuration
	if c.connIndex == 0 && !registrationDetails.TunnelIsRemotelyManaged {
		if tunnelConfig, err := tunnelConfigGetter.GetConfigJSON(); err == nil {
			if err := rpcClient.SendLocalConfiguration(ctx, tunnelConfig, c.observer); err != nil {
				c.observer.log.Err(err).Msg("unable to send local configuration")
			}
		} else {
			c.observer.log.Err(err).Msg("failed to obtain current configuration")
		}
	}

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
