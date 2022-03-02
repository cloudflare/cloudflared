package supervisor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/orchestration"
	quicpogs "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/retry"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	dialTimeout              = 15 * time.Second
	FeatureSerializedHeaders = "serialized_headers"
	FeatureQuickReconnects   = "quick_reconnects"
)

type TunnelConfig struct {
	GracePeriod     time.Duration
	ReplaceExisting bool
	OSArch          string
	ClientID        string
	CloseConnOnce   *sync.Once // Used to close connectedSignal no more than once
	EdgeAddrs       []string
	Region          string
	HAConnections   int
	IncidentLookup  IncidentLookup
	IsAutoupdated   bool
	LBPool          string
	Tags            []tunnelpogs.Tag
	Log             *zerolog.Logger
	LogTransport    *zerolog.Logger
	Observer        *connection.Observer
	ReportedVersion string
	Retries         uint
	RunFromTerminal bool

	NamedTunnel      *connection.NamedTunnelProperties
	ClassicTunnel    *connection.ClassicTunnelProperties
	MuxerConfig      *connection.MuxerConfig
	ProtocolSelector connection.ProtocolSelector
	EdgeTLSConfigs   map[connection.Protocol]*tls.Config
}

func (c *TunnelConfig) registrationOptions(connectionID uint8, OriginLocalIP string, uuid uuid.UUID) *tunnelpogs.RegistrationOptions {
	policy := tunnelrpc.ExistingTunnelPolicy_balance
	if c.HAConnections <= 1 && c.LBPool == "" {
		policy = tunnelrpc.ExistingTunnelPolicy_disconnect
	}
	return &tunnelpogs.RegistrationOptions{
		ClientID:             c.ClientID,
		Version:              c.ReportedVersion,
		OS:                   c.OSArch,
		ExistingTunnelPolicy: policy,
		PoolName:             c.LBPool,
		Tags:                 c.Tags,
		ConnectionID:         connectionID,
		OriginLocalIP:        OriginLocalIP,
		IsAutoupdated:        c.IsAutoupdated,
		RunFromTerminal:      c.RunFromTerminal,
		CompressionQuality:   uint64(c.MuxerConfig.CompressionSetting),
		UUID:                 uuid.String(),
		Features:             c.SupportedFeatures(),
	}
}

func (c *TunnelConfig) connectionOptions(originLocalAddr string, numPreviousAttempts uint8) *tunnelpogs.ConnectionOptions {
	// attempt to parse out origin IP, but don't fail since it's informational field
	host, _, _ := net.SplitHostPort(originLocalAddr)
	originIP := net.ParseIP(host)

	return &tunnelpogs.ConnectionOptions{
		Client:              c.NamedTunnel.Client,
		OriginLocalIP:       originIP,
		ReplaceExisting:     c.ReplaceExisting,
		CompressionQuality:  uint8(c.MuxerConfig.CompressionSetting),
		NumPreviousAttempts: numPreviousAttempts,
	}
}

func (c *TunnelConfig) SupportedFeatures() []string {
	features := []string{FeatureSerializedHeaders}
	if c.NamedTunnel == nil {
		features = append(features, FeatureQuickReconnects)
	}
	return features
}

func StartTunnelDaemon(
	ctx context.Context,
	config *TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	connectedSignal *signal.Signal,
	reconnectCh chan ReconnectSignal,
	graceShutdownC <-chan struct{},
) error {
	s, err := NewSupervisor(config, orchestrator, reconnectCh, graceShutdownC)
	if err != nil {
		return err
	}
	return s.Run(ctx, connectedSignal)
}

func ServeTunnelLoop(
	ctx context.Context,
	credentialManager *reconnectCredentialManager,
	config *TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	addr *allregions.EdgeAddr,
	connAwareLogger *ConnAwareLogger,
	connIndex uint8,
	connectedSignal *signal.Signal,
	cloudflaredUUID uuid.UUID,
	reconnectCh chan ReconnectSignal,
	gracefulShutdownC <-chan struct{},
) error {
	haConnections.Inc()
	defer haConnections.Dec()

	logger := config.Log.With().Uint8(connection.LogFieldConnIndex, connIndex).Logger()
	connLog := connAwareLogger.ReplaceLogger(&logger)

	protocolFallback := &protocolFallback{
		retry.BackoffHandler{MaxRetries: config.Retries},
		config.ProtocolSelector.Current(),
		false,
	}
	connectedFuse := h2mux.NewBooleanFuse()
	go func() {
		if connectedFuse.Await() {
			connectedSignal.Notify()
		}
	}()
	// Ensure the above goroutine will terminate if we return without connecting
	defer connectedFuse.Fuse(false)
	// Each connection to keep its own copy of protocol, because individual connections might fallback
	// to another protocol when a particular metal doesn't support new protocol
	for {
		err, recoverable := ServeTunnel(
			ctx,
			connLog,
			credentialManager,
			config,
			orchestrator,
			addr,
			connIndex,
			connectedFuse,
			protocolFallback,
			cloudflaredUUID,
			reconnectCh,
			protocolFallback.protocol,
			gracefulShutdownC,
		)
		if !recoverable {
			return err
		}

		config.Observer.SendReconnect(connIndex)

		duration, ok := protocolFallback.GetMaxBackoffDuration(ctx)
		if !ok {
			return err
		}
		connLog.Logger().Info().Msgf("Retrying connection in up to %s seconds", duration)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-gracefulShutdownC:
			return nil
		case <-protocolFallback.BackoffTimer():
			if !selectNextProtocol(
				connLog.Logger(),
				protocolFallback,
				config.ProtocolSelector,
				err,
			) {
				return err
			}
		}
	}
}

// protocolFallback is a wrapper around backoffHandler that will try fallback option when backoff reaches
// max retries
type protocolFallback struct {
	retry.BackoffHandler
	protocol   connection.Protocol
	inFallback bool
}

func (pf *protocolFallback) reset() {
	pf.ResetNow()
	pf.inFallback = false
}

func (pf *protocolFallback) fallback(fallback connection.Protocol) {
	pf.ResetNow()
	pf.protocol = fallback
	pf.inFallback = true
}

// selectNextProtocol picks connection protocol for the next retry iteration,
// returns true if it was able to pick the protocol, false if we are out of options and should stop retrying
func selectNextProtocol(
	connLog *zerolog.Logger,
	protocolBackoff *protocolFallback,
	selector connection.ProtocolSelector,
	cause error,
) bool {
	var idleTimeoutError *quic.IdleTimeoutError
	isNetworkActivityTimeout := errors.As(cause, &idleTimeoutError)
	_, hasFallback := selector.Fallback()

	if protocolBackoff.ReachedMaxRetries() || (hasFallback && isNetworkActivityTimeout) {
		fallback, hasFallback := selector.Fallback()
		if !hasFallback {
			return false
		}
		// Already using fallback protocol, no point to retry
		if protocolBackoff.protocol == fallback {
			return false
		}
		connLog.Info().Msgf("Switching to fallback protocol %s", fallback)
		protocolBackoff.fallback(fallback)
	} else if !protocolBackoff.inFallback {
		current := selector.Current()
		if protocolBackoff.protocol != current {
			protocolBackoff.protocol = current
			connLog.Info().Msgf("Changing protocol to %s", current)
		}
	}
	return true
}

// ServeTunnel runs a single tunnel connection, returns nil on graceful shutdown,
// on error returns a flag indicating if error can be retried
func ServeTunnel(
	ctx context.Context,
	connLog *ConnAwareLogger,
	credentialManager *reconnectCredentialManager,
	config *TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	addr *allregions.EdgeAddr,
	connIndex uint8,
	fuse *h2mux.BooleanFuse,
	backoff *protocolFallback,
	cloudflaredUUID uuid.UUID,
	reconnectCh chan ReconnectSignal,
	protocol connection.Protocol,
	gracefulShutdownC <-chan struct{},
) (err error, recoverable bool) {
	// Treat panics as recoverable errors
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("ServeTunnel: %v", r)
			}
			err = errors.Wrapf(err, "stack trace: %s", string(debug.Stack()))
			recoverable = true
		}
	}()

	defer config.Observer.SendDisconnect(connIndex)
	err, recoverable = serveTunnel(
		ctx,
		connLog,
		credentialManager,
		config,
		orchestrator,
		addr,
		connIndex,
		fuse,
		backoff,
		cloudflaredUUID,
		reconnectCh,
		protocol,
		gracefulShutdownC,
	)

	if err != nil {
		switch err := err.(type) {
		case connection.DupConnRegisterTunnelError:
			connLog.ConnAwareLogger().Err(err).Msg("Unable to establish connection.")
			// don't retry this connection anymore, let supervisor pick a new address
			return err, false
		case connection.ServerRegisterTunnelError:
			connLog.ConnAwareLogger().Err(err).Msg("Register tunnel error from server side")
			// Don't send registration error return from server to Sentry. They are
			// logged on server side
			if incidents := config.IncidentLookup.ActiveIncidents(); len(incidents) > 0 {
				connLog.ConnAwareLogger().Msg(activeIncidentsMsg(incidents))
			}
			return err.Cause, !err.Permanent
		case ReconnectSignal:
			connLog.Logger().Info().
				Uint8(connection.LogFieldConnIndex, connIndex).
				Msgf("Restarting connection due to reconnect signal in %s", err.Delay)
			err.DelayBeforeReconnect()
			return err, true
		default:
			if err == context.Canceled {
				connLog.Logger().Debug().Err(err).Msgf("Serve tunnel error")
				return err, false
			}
			connLog.ConnAwareLogger().Err(err).Msgf("Serve tunnel error")
			_, permanent := err.(unrecoverableError)
			return err, !permanent
		}
	}
	return nil, false
}

func serveTunnel(
	ctx context.Context,
	connLog *ConnAwareLogger,
	credentialManager *reconnectCredentialManager,
	config *TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	addr *allregions.EdgeAddr,
	connIndex uint8,
	fuse *h2mux.BooleanFuse,
	backoff *protocolFallback,
	cloudflaredUUID uuid.UUID,
	reconnectCh chan ReconnectSignal,
	protocol connection.Protocol,
	gracefulShutdownC <-chan struct{},
) (err error, recoverable bool) {
	connectedFuse := &connectedFuse{
		fuse:    fuse,
		backoff: backoff,
	}
	controlStream := connection.NewControlStream(
		config.Observer,
		connectedFuse,
		config.NamedTunnel,
		connIndex,
		nil,
		gracefulShutdownC,
		config.GracePeriod,
	)

	switch protocol {
	case connection.QUIC, connection.QUICWarp:
		connOptions := config.connectionOptions(addr.UDP.String(), uint8(backoff.Retries()))
		return ServeQUIC(ctx,
			addr.UDP,
			config,
			orchestrator,
			connLog,
			connOptions,
			controlStream,
			connIndex,
			reconnectCh,
			gracefulShutdownC)

	case connection.HTTP2, connection.HTTP2Warp:
		edgeConn, err := edgediscovery.DialEdge(ctx, dialTimeout, config.EdgeTLSConfigs[protocol], addr.TCP)
		if err != nil {
			connLog.ConnAwareLogger().Err(err).Msg("Unable to establish connection with Cloudflare edge")
			return err, true
		}

		connOptions := config.connectionOptions(edgeConn.LocalAddr().String(), uint8(backoff.Retries()))
		if err := ServeHTTP2(
			ctx,
			connLog,
			config,
			orchestrator,
			edgeConn,
			connOptions,
			controlStream,
			connIndex,
			gracefulShutdownC,
			reconnectCh,
		); err != nil {
			return err, false
		}

	default:
		edgeConn, err := edgediscovery.DialEdge(ctx, dialTimeout, config.EdgeTLSConfigs[protocol], addr.TCP)
		if err != nil {
			connLog.ConnAwareLogger().Err(err).Msg("Unable to establish connection with Cloudflare edge")
			return err, true
		}

		if err := ServeH2mux(
			ctx,
			connLog,
			credentialManager,
			config,
			orchestrator,
			edgeConn,
			connIndex,
			connectedFuse,
			cloudflaredUUID,
			reconnectCh,
			gracefulShutdownC,
		); err != nil {
			return err, false
		}
	}
	return
}

type unrecoverableError struct {
	err error
}

func (r unrecoverableError) Error() string {
	return r.err.Error()
}

func ServeH2mux(
	ctx context.Context,
	connLog *ConnAwareLogger,
	credentialManager *reconnectCredentialManager,
	config *TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	edgeConn net.Conn,
	connIndex uint8,
	connectedFuse *connectedFuse,
	cloudflaredUUID uuid.UUID,
	reconnectCh chan ReconnectSignal,
	gracefulShutdownC <-chan struct{},
) error {
	connLog.Logger().Debug().Msgf("Connecting via h2mux")
	// Returns error from parsing the origin URL or handshake errors
	handler, err, recoverable := connection.NewH2muxConnection(
		orchestrator,
		config.GracePeriod,
		config.MuxerConfig,
		edgeConn,
		connIndex,
		config.Observer,
		gracefulShutdownC,
	)
	if err != nil {
		if !recoverable {
			return unrecoverableError{err}
		}
		return err
	}

	errGroup, serveCtx := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		if config.NamedTunnel != nil {
			connOptions := config.connectionOptions(edgeConn.LocalAddr().String(), uint8(connectedFuse.backoff.Retries()))
			return handler.ServeNamedTunnel(serveCtx, config.NamedTunnel, connOptions, connectedFuse)
		}
		registrationOptions := config.registrationOptions(connIndex, edgeConn.LocalAddr().String(), cloudflaredUUID)
		return handler.ServeClassicTunnel(serveCtx, config.ClassicTunnel, credentialManager, registrationOptions, connectedFuse)
	})

	errGroup.Go(func() error {
		return listenReconnect(serveCtx, reconnectCh, gracefulShutdownC)
	})

	return errGroup.Wait()
}

func ServeHTTP2(
	ctx context.Context,
	connLog *ConnAwareLogger,
	config *TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	tlsServerConn net.Conn,
	connOptions *tunnelpogs.ConnectionOptions,
	controlStreamHandler connection.ControlStreamHandler,
	connIndex uint8,
	gracefulShutdownC <-chan struct{},
	reconnectCh chan ReconnectSignal,
) error {
	connLog.Logger().Debug().Msgf("Connecting via http2")
	h2conn := connection.NewHTTP2Connection(
		tlsServerConn,
		orchestrator,
		connOptions,
		config.Observer,
		connIndex,
		controlStreamHandler,
		config.Log,
	)

	errGroup, serveCtx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		return h2conn.Serve(serveCtx)
	})

	errGroup.Go(func() error {
		err := listenReconnect(serveCtx, reconnectCh, gracefulShutdownC)
		if err != nil {
			// forcefully break the connection (this is only used for testing)
			_ = tlsServerConn.Close()
		}
		return err
	})

	return errGroup.Wait()
}

func ServeQUIC(
	ctx context.Context,
	edgeAddr *net.UDPAddr,
	config *TunnelConfig,
	orchestrator *orchestration.Orchestrator,
	connLogger *ConnAwareLogger,
	connOptions *tunnelpogs.ConnectionOptions,
	controlStreamHandler connection.ControlStreamHandler,
	connIndex uint8,
	reconnectCh chan ReconnectSignal,
	gracefulShutdownC <-chan struct{},
) (err error, recoverable bool) {
	tlsConfig := config.EdgeTLSConfigs[connection.QUIC]
	quicConfig := &quic.Config{
		HandshakeIdleTimeout:  quicpogs.HandshakeIdleTimeout,
		MaxIdleTimeout:        quicpogs.MaxIdleTimeout,
		MaxIncomingStreams:    connection.MaxConcurrentStreams,
		MaxIncomingUniStreams: connection.MaxConcurrentStreams,
		KeepAlive:             true,
		EnableDatagrams:       true,
		MaxDatagramFrameSize:  quicpogs.MaxDatagramFrameSize,
		Tracer:                quicpogs.NewClientTracer(connLogger.Logger(), connIndex),
	}

	quicConn, err := connection.NewQUICConnection(
		quicConfig,
		edgeAddr,
		tlsConfig,
		orchestrator,
		connOptions,
		controlStreamHandler,
		connLogger.Logger())
	if err != nil {
		connLogger.ConnAwareLogger().Err(err).Msgf("Failed to create new quic connection")
		return err, true
	}

	errGroup, serveCtx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		err := quicConn.Serve(serveCtx)
		if err != nil {
			connLogger.ConnAwareLogger().Err(err).Msg("Failed to serve quic connection")
		}
		return err
	})

	errGroup.Go(func() error {
		err := listenReconnect(serveCtx, reconnectCh, gracefulShutdownC)
		if err != nil {
			// forcefully break the connection (this is only used for testing)
			quicConn.Close()
		}
		return err
	})

	return errGroup.Wait(), false
}

func listenReconnect(ctx context.Context, reconnectCh <-chan ReconnectSignal, gracefulShutdownCh <-chan struct{}) error {
	select {
	case reconnect := <-reconnectCh:
		return reconnect
	case <-gracefulShutdownCh:
		return nil
	case <-ctx.Done():
		return nil
	}
}

type connectedFuse struct {
	fuse    *h2mux.BooleanFuse
	backoff *protocolFallback
}

func (cf *connectedFuse) Connected() {
	cf.fuse.Fuse(true)
	cf.backoff.reset()
}

func (cf *connectedFuse) IsConnected() bool {
	return cf.fuse.Value()
}

func activeIncidentsMsg(incidents []Incident) string {
	preamble := "There is an active Cloudflare incident that may be related:"
	if len(incidents) > 1 {
		preamble = "There are active Cloudflare incidents that may be related:"
	}
	incidentStrings := []string{}
	for _, incident := range incidents {
		incidentString := fmt.Sprintf("%s (%s)", incident.Name, incident.URL())
		incidentStrings = append(incidentStrings, incidentString)
	}
	return preamble + " " + strings.Join(incidentStrings, "; ")
}
