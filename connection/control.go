package connection

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/management"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// registerClient derives a named tunnel rpc client that can then be used to register and unregister connections.
type registerClientFunc func(context.Context, io.ReadWriteCloser, time.Duration) tunnelrpc.RegistrationClient

type controlStream struct {
	observer *Observer

	connectedFuse    ConnectedFuse
	tunnelProperties *TunnelProperties
	connIndex        uint8
	edgeAddress      net.IP
	protocol         Protocol

	registerClientFunc registerClientFunc
	registerTimeout    time.Duration

	gracefulShutdownC <-chan struct{}
	gracePeriod       time.Duration
	stoppedGracefully bool
}

// ControlStreamHandler registers connections with origintunneld and initiates graceful shutdown.
type ControlStreamHandler interface {
	// ServeControlStream handles the control plane of the transport in the current goroutine calling this
	ServeControlStream(ctx context.Context, rw io.ReadWriteCloser, connOptions *pogs.ConnectionOptions, tunnelConfigGetter TunnelConfigJSONGetter) error
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
	tunnelProperties *TunnelProperties,
	connIndex uint8,
	edgeAddress net.IP,
	registerClientFunc registerClientFunc,
	registerTimeout time.Duration,
	gracefulShutdownC <-chan struct{},
	gracePeriod time.Duration,
	protocol Protocol,
) ControlStreamHandler {
	if registerClientFunc == nil {
		registerClientFunc = tunnelrpc.NewRegistrationClient
	}
	return &controlStream{
		observer:           observer,
		connectedFuse:      connectedFuse,
		tunnelProperties:   tunnelProperties,
		registerClientFunc: registerClientFunc,
		registerTimeout:    registerTimeout,
		connIndex:          connIndex,
		edgeAddress:        edgeAddress,
		gracefulShutdownC:  gracefulShutdownC,
		gracePeriod:        gracePeriod,
		protocol:           protocol,
	}
}

func (c *controlStream) ServeControlStream(
	ctx context.Context,
	rw io.ReadWriteCloser,
	connOptions *pogs.ConnectionOptions,
	tunnelConfigGetter TunnelConfigJSONGetter,
) error {
	registrationClient := c.registerClientFunc(ctx, rw, c.registerTimeout)
	c.observer.logConnecting(c.connIndex, c.edgeAddress, c.protocol)
	registrationDetails, err := registrationClient.RegisterConnection(
		ctx,
		c.tunnelProperties.Credentials.Auth(),
		c.tunnelProperties.Credentials.TunnelID,
		connOptions,
		c.connIndex,
		c.edgeAddress)
	if err != nil {
		defer registrationClient.Close()
		if err.Error() == DuplicateConnectionError {
			c.observer.metrics.regFail.WithLabelValues("dup_edge_conn", "registerConnection").Inc()
			return errDuplicationConnection
		}
		c.observer.metrics.regFail.WithLabelValues("server_error", "registerConnection").Inc()
		return serverRegistrationErrorFromRPC(err)
	}
	c.observer.metrics.regSuccess.WithLabelValues("registerConnection").Inc()

	c.observer.logConnected(registrationDetails.UUID, c.connIndex, registrationDetails.Location, c.edgeAddress, c.protocol)
	c.observer.sendConnectedEvent(c.connIndex, c.protocol, registrationDetails.Location, c.edgeAddress)
	c.connectedFuse.Connected()

	// if conn index is 0 and tunnel is not remotely managed, then send local ingress rules configuration
	if c.connIndex == 0 && !registrationDetails.TunnelIsRemotelyManaged {
		if tunnelConfig, err := tunnelConfigGetter.GetConfigJSON(); err == nil {
			if err := registrationClient.SendLocalConfiguration(ctx, tunnelConfig); err != nil {
				c.observer.metrics.localConfigMetrics.pushesErrors.Inc()
				c.observer.log.Err(err).Msg("unable to send local configuration")
			}
			c.observer.metrics.localConfigMetrics.pushes.Inc()
		} else {
			c.observer.log.Err(err).Msg("failed to obtain current configuration")
		}
	}

	return c.waitForUnregister(ctx, registrationClient)
}

func (c *controlStream) waitForUnregister(ctx context.Context, registrationClient tunnelrpc.RegistrationClient) error {
	// wait for connection termination or start of graceful shutdown
	defer registrationClient.Close()
	var shutdownError error
	select {
	case <-ctx.Done():
		shutdownError = ctx.Err()
		break
	case <-c.gracefulShutdownC:
		c.stoppedGracefully = true
	}

	c.observer.sendUnregisteringEvent(c.connIndex)
	err := registrationClient.GracefulShutdown(ctx, c.gracePeriod)
	if err != nil {
		return errors.Wrap(err, "Error shutting down control stream")
	}
	c.observer.log.Info().
		Int(management.EventTypeKey, int(management.Cloudflared)).
		Uint8(LogFieldConnIndex, c.connIndex).
		IPAddr(LogFieldIPAddress, c.edgeAddress).
		Msg("Unregistered tunnel connection")
	return shutdownError
}

func (c *controlStream) IsStopped() bool {
	return c.stoppedGracefully
}
