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
	"github.com/cloudflare/cloudflared/packet"
	quicpogs "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/retry"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

const (
	dialTimeout              = 15 * time.Second
	FeatureSerializedHeaders = "serialized_headers"
	FeatureQuickReconnects   = "quick_reconnects"
	FeatureAllowRemoteConfig = "allow_remote_config"
	FeatureDatagramV2        = "support_datagram_v2"
	FeaturePostQuantum       = "postquantum"
)

type TunnelConfig struct {
	GracePeriod     time.Duration
	ReplaceExisting bool
	OSArch          string
	ClientID        string
	CloseConnOnce   *sync.Once // Used to close connectedSignal no more than once
	EdgeAddrs       []string
	Region          string
	EdgeIPVersion   allregions.ConfigIPVersion
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

	NeedPQ bool

	// Index into PQKexes of post-quantum kex to use if NeedPQ is set.
	PQKexIdx int

	NamedTunnel      *connection.NamedTunnelProperties
	ClassicTunnel    *connection.ClassicTunnelProperties
	MuxerConfig      *connection.MuxerConfig
	ProtocolSelector connection.ProtocolSelector
	EdgeTLSConfigs   map[connection.Protocol]*tls.Config
	PacketConfig     *packet.GlobalRouterConfig
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

// EdgeAddrHandler provides a mechanism switch between behaviors in ServeTunnel
// for handling the errors when attempting to make edge connections.
type EdgeAddrHandler interface {
	// ShouldGetNewAddress will check the edge connection error and determine if
	// the edge address should be replaced with a new one. Also, will return if the
	// error should be recognized as a connectivity error, or otherwise, a general
	// application error.
	ShouldGetNewAddress(err error) (needsNewAddress bool, isConnectivityError bool)
}

// DefaultAddrFallback will always return false for isConnectivityError since this
// handler is a way to provide the legacy behavior in the new edge discovery algorithm.
type DefaultAddrFallback struct {
	edgeErrors int
}

func (f DefaultAddrFallback) ShouldGetNewAddress(err error) (needsNewAddress bool, isConnectivityError bool) {
	switch err.(type) {
	case nil: // maintain current IP address
	// DupConnRegisterTunnelError should indicate to get a new address immediately
	case connection.DupConnRegisterTunnelError:
		return true, false
	// Try the next address if it was a quic.IdleTimeoutError
	case *quic.IdleTimeoutError,
		edgediscovery.DialError,
		*connection.EdgeQuicDialError:
		// Wait for two failures before falling back to a new address
		f.edgeErrors++
		if f.edgeErrors >= 2 {
			f.edgeErrors = 0
			return true, false
		}
	default: // maintain current IP address
	}
	return false, false
}

// IPAddrFallback will have more conditions to fall back to a new address for certain
// edge connection errors. This means that this handler will return true for isConnectivityError
// for more cases like duplicate connection register and edge quic dial errors.
type IPAddrFallback struct{}

func (f IPAddrFallback) ShouldGetNewAddress(err error) (needsNewAddress bool, isConnectivityError bool) {
	switch err.(type) {
	case nil: // maintain current IP address
	// Try the next address if it was a quic.IdleTimeoutError
	// DupConnRegisterTunnelError needs to also receive a new ip address
	case connection.DupConnRegisterTunnelError,
		*quic.IdleTimeoutError:
		return true, false
	// Network problems should be retried with new address immediately and report
	// as connectivity error
	case edgediscovery.DialError, *connection.EdgeQuicDialError:
		return true, true
	default: // maintain current IP address
	}
	return false, false
}

type EdgeTunnelServer struct {
	config            *TunnelConfig
	cloudflaredUUID   uuid.UUID
	orchestrator      *orchestration.Orchestrator
	credentialManager *reconnectCredentialManager
	edgeAddrHandler   EdgeAddrHandler
	edgeAddrs         *edgediscovery.Edge
	reconnectCh       chan ReconnectSignal
	gracefulShutdownC <-chan struct{}
	tracker           *tunnelstate.ConnTracker

	connAwareLogger *ConnAwareLogger
}

func (e *EdgeTunnelServer) Serve(ctx context.Context, connIndex uint8, protocolFallback *protocolFallback, connectedSignal *signal.Signal) error {
	haConnections.Inc()
	defer haConnections.Dec()

	connectedFuse := h2mux.NewBooleanFuse()
	go func() {
		if connectedFuse.Await() {
			connectedSignal.Notify()
		}
	}()
	// Ensure the above goroutine will terminate if we return without connecting
	defer connectedFuse.Fuse(false)

	// Fetch IP address to associated connection index
	addr, err := e.edgeAddrs.GetAddr(int(connIndex))
	switch err.(type) {
	case nil: // no error
	case edgediscovery.ErrNoAddressesLeft:
		return err
	default:
		return err
	}

	logger := e.config.Log.With().
		IPAddr(connection.LogFieldIPAddress, addr.UDP.IP).
		Uint8(connection.LogFieldConnIndex, connIndex).
		Logger()
	connLog := e.connAwareLogger.ReplaceLogger(&logger)
	// Each connection to keep its own copy of protocol, because individual connections might fallback
	// to another protocol when a particular metal doesn't support new protocol
	// Each connection can also have it's own IP version because individual connections might fallback
	// to another IP version.
	err, recoverable := e.serveTunnel(
		ctx,
		connLog,
		addr,
		connIndex,
		connectedFuse,
		protocolFallback,
		protocolFallback.protocol,
	)

	// If the connection is recoverable, we want to maintain the same IP
	// but backoff a reconnect with some duration.
	if recoverable {
		duration, ok := protocolFallback.GetMaxBackoffDuration(ctx)
		if !ok {
			return err
		}

		e.config.Observer.SendReconnect(connIndex)
		connLog.Logger().Info().Msgf("Retrying connection in up to %s", duration)
	}

	// Check if the connection error was from an IP issue with the host or
	// establishing a connection to the edge and if so, rotate the IP address.
	yes, hasConnectivityError := e.edgeAddrHandler.ShouldGetNewAddress(err)
	if yes {
		if _, err := e.edgeAddrs.GetDifferentAddr(int(connIndex), hasConnectivityError); err != nil {
			return err
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.gracefulShutdownC:
		return nil
	case <-protocolFallback.BackoffTimer():
		if !recoverable {
			return err
		}

		// If a single connection has connected with the current protocol, we know we know we don't have to fallback
		// to a different protocol.
		if e.tracker.HasConnectedWith(e.config.ProtocolSelector.Current()) {
			return err
		}

		if !selectNextProtocol(
			connLog.Logger(),
			protocolFallback,
			e.config.ProtocolSelector,
			err,
		) {
			return err
		}
	}

	return err
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
	isQuicBroken := isQuicBroken(cause)
	_, hasFallback := selector.Fallback()

	if protocolBackoff.ReachedMaxRetries() || (hasFallback && isQuicBroken) {
		if isQuicBroken {
			connLog.Warn().Msg("If this log occurs persistently, and cloudflared is unable to connect to " +
				"Cloudflare Network with `quic` protocol, then most likely your machine/network is getting its egress " +
				"UDP to port 7844 (or others) blocked or dropped. Make sure to allow egress connectivity as per " +
				"https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/configuration/ports-and-ips/\n" +
				"If you are using private routing to this Tunnel, then UDP (and Private DNS Resolution) will not work " +
				"unless your cloudflared can connect with Cloudflare Network with `quic`.")
		}

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

func isQuicBroken(cause error) bool {
	var idleTimeoutError *quic.IdleTimeoutError
	if errors.As(cause, &idleTimeoutError) {
		return true
	}

	var transportError *quic.TransportError
	if errors.As(cause, &transportError) && strings.Contains(cause.Error(), "operation not permitted") {
		return true
	}

	return false
}

// ServeTunnel runs a single tunnel connection, returns nil on graceful shutdown,
// on error returns a flag indicating if error can be retried
func (e *EdgeTunnelServer) serveTunnel(
	ctx context.Context,
	connLog *ConnAwareLogger,
	addr *allregions.EdgeAddr,
	connIndex uint8,
	fuse *h2mux.BooleanFuse,
	backoff *protocolFallback,
	protocol connection.Protocol,
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

	defer e.config.Observer.SendDisconnect(connIndex)
	err, recoverable = e.serveConnection(
		ctx,
		connLog,
		addr,
		connIndex,
		fuse,
		backoff,
		protocol,
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
			if incidents := e.config.IncidentLookup.ActiveIncidents(); len(incidents) > 0 {
				connLog.ConnAwareLogger().Msg(activeIncidentsMsg(incidents))
			}
			return err.Cause, !err.Permanent
		case *connection.EdgeQuicDialError:
			return err, true
		case ReconnectSignal:
			connLog.Logger().Info().
				IPAddr(connection.LogFieldIPAddress, addr.UDP.IP).
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

func (e *EdgeTunnelServer) serveConnection(
	ctx context.Context,
	connLog *ConnAwareLogger,
	addr *allregions.EdgeAddr,
	connIndex uint8,
	fuse *h2mux.BooleanFuse,
	backoff *protocolFallback,
	protocol connection.Protocol,
) (err error, recoverable bool) {
	connectedFuse := &connectedFuse{
		fuse:    fuse,
		backoff: backoff,
	}
	controlStream := connection.NewControlStream(
		e.config.Observer,
		connectedFuse,
		e.config.NamedTunnel,
		connIndex,
		addr.UDP.IP,
		nil,
		e.gracefulShutdownC,
		e.config.GracePeriod,
		protocol,
	)

	switch protocol {
	case connection.QUIC, connection.QUICWarp:
		connOptions := e.config.connectionOptions(addr.UDP.String(), uint8(backoff.Retries()))
		return e.serveQUIC(ctx,
			addr.UDP,
			connLog,
			connOptions,
			controlStream,
			connIndex)

	case connection.HTTP2, connection.HTTP2Warp:
		edgeConn, err := edgediscovery.DialEdge(ctx, dialTimeout, e.config.EdgeTLSConfigs[protocol], addr.TCP)
		if err != nil {
			connLog.ConnAwareLogger().Err(err).Msg("Unable to establish connection with Cloudflare edge")
			return err, true
		}

		connOptions := e.config.connectionOptions(edgeConn.LocalAddr().String(), uint8(backoff.Retries()))
		if err := e.serveHTTP2(
			ctx,
			connLog,
			edgeConn,
			connOptions,
			controlStream,
			connIndex,
		); err != nil {
			return err, false
		}

	default:
		edgeConn, err := edgediscovery.DialEdge(ctx, dialTimeout, e.config.EdgeTLSConfigs[protocol], addr.TCP)
		if err != nil {
			connLog.ConnAwareLogger().Err(err).Msg("Unable to establish connection with Cloudflare edge")
			return err, true
		}

		if err := e.serveH2mux(
			ctx,
			connLog,
			edgeConn,
			connIndex,
			connectedFuse,
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

func (e *EdgeTunnelServer) serveH2mux(
	ctx context.Context,
	connLog *ConnAwareLogger,
	edgeConn net.Conn,
	connIndex uint8,
	connectedFuse *connectedFuse,
) error {
	if e.config.NeedPQ {
		return unrecoverableError{errors.New("H2Mux transport does not support post-quantum")}
	}
	connLog.Logger().Debug().Msgf("Connecting via h2mux")
	// Returns error from parsing the origin URL or handshake errors
	handler, err, recoverable := connection.NewH2muxConnection(
		e.orchestrator,
		e.config.GracePeriod,
		e.config.MuxerConfig,
		edgeConn,
		connIndex,
		e.config.Observer,
		e.gracefulShutdownC,
		e.config.Log,
	)
	if err != nil {
		if !recoverable {
			return unrecoverableError{err}
		}
		return err
	}

	errGroup, serveCtx := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		if e.config.NamedTunnel != nil {
			connOptions := e.config.connectionOptions(edgeConn.LocalAddr().String(), uint8(connectedFuse.backoff.Retries()))
			return handler.ServeNamedTunnel(serveCtx, e.config.NamedTunnel, connOptions, connectedFuse)
		}
		registrationOptions := e.config.registrationOptions(connIndex, edgeConn.LocalAddr().String(), e.cloudflaredUUID)
		return handler.ServeClassicTunnel(serveCtx, e.config.ClassicTunnel, e.credentialManager, registrationOptions, connectedFuse)
	})

	errGroup.Go(func() error {
		err := listenReconnect(serveCtx, e.reconnectCh, e.gracefulShutdownC)
		if err != nil {
			// forcefully break the connection (this is only used for testing)
			// errgroup will return context canceled for the handler.ServeClassicTunnel
			connLog.Logger().Debug().Msg("Forcefully breaking h2mux connection")
		}
		return err
	})

	return errGroup.Wait()
}

func (e *EdgeTunnelServer) serveHTTP2(
	ctx context.Context,
	connLog *ConnAwareLogger,
	tlsServerConn net.Conn,
	connOptions *tunnelpogs.ConnectionOptions,
	controlStreamHandler connection.ControlStreamHandler,
	connIndex uint8,
) error {
	if e.config.NeedPQ {
		return unrecoverableError{errors.New("HTTP/2 transport does not support post-quantum")}
	}

	connLog.Logger().Debug().Msgf("Connecting via http2")
	h2conn := connection.NewHTTP2Connection(
		tlsServerConn,
		e.orchestrator,
		connOptions,
		e.config.Observer,
		connIndex,
		controlStreamHandler,
		e.config.Log,
	)

	errGroup, serveCtx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		return h2conn.Serve(serveCtx)
	})

	errGroup.Go(func() error {
		err := listenReconnect(serveCtx, e.reconnectCh, e.gracefulShutdownC)
		if err != nil {
			// forcefully break the connection (this is only used for testing)
			// errgroup will return context canceled for the h2conn.Serve
			connLog.Logger().Debug().Msg("Forcefully breaking http2 connection")
		}
		return err
	})

	return errGroup.Wait()
}

func (e *EdgeTunnelServer) serveQUIC(
	ctx context.Context,
	edgeAddr *net.UDPAddr,
	connLogger *ConnAwareLogger,
	connOptions *tunnelpogs.ConnectionOptions,
	controlStreamHandler connection.ControlStreamHandler,
	connIndex uint8,
) (err error, recoverable bool) {
	tlsConfig := e.config.EdgeTLSConfigs[connection.QUIC]

	if e.config.NeedPQ {
		// If the user passes the -post-quantum flag, we override
		// CurvePreferences to only support hybrid post-quantum key agreements.
		cs := make([]tls.CurveID, len(PQKexes))
		copy(cs, PQKexes[:])

		// It is unclear whether Kyber512 or Kyber768 will become the standard.
		// Kyber768 is a bit bigger (and doesn't fit in one initial
		// datagram anymore). We're enabling both, but pick randomly which
		// one to put first. (TLS will use the first one in the list
		// and allows a fallback to the second.)
		cs[0], cs[e.config.PQKexIdx] = cs[e.config.PQKexIdx], cs[0]
		tlsConfig.CurvePreferences = cs
	}

	quicConfig := &quic.Config{
		HandshakeIdleTimeout:  quicpogs.HandshakeIdleTimeout,
		MaxIdleTimeout:        quicpogs.MaxIdleTimeout,
		KeepAlivePeriod:       quicpogs.MaxIdlePingPeriod,
		MaxIncomingStreams:    connection.MaxConcurrentStreams,
		MaxIncomingUniStreams: connection.MaxConcurrentStreams,
		EnableDatagrams:       true,
		MaxDatagramFrameSize:  quicpogs.MaxDatagramFrameSize,
		Tracer:                quicpogs.NewClientTracer(connLogger.Logger(), connIndex),
	}

	quicConn, err := connection.NewQUICConnection(
		quicConfig,
		edgeAddr,
		tlsConfig,
		e.orchestrator,
		connOptions,
		controlStreamHandler,
		connLogger.Logger(),
		e.config.PacketConfig)
	if err != nil {
		if e.config.NeedPQ {
			handlePQTunnelError(err, e.config)
		}

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
		err := listenReconnect(serveCtx, e.reconnectCh, e.gracefulShutdownC)
		if err != nil {
			// forcefully break the connection (this is only used for testing)
			// errgroup will return context canceled for the quicConn.Serve
			connLogger.Logger().Debug().Msg("Forcefully breaking quic connection")
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
