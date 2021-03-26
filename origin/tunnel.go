package origin

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
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/retry"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	dialTimeout              = 15 * time.Second
	lbProbeUserAgentPrefix   = "Mozilla/5.0 (compatible; Cloudflare-Traffic-Manager/1.0; +https://www.cloudflare.com/traffic-manager/;"
	FeatureSerializedHeaders = "serialized_headers"
	FeatureQuickReconnects   = "quick_reconnects"
)

type rpcName string

const (
	reconnect    rpcName = "reconnect"
	authenticate rpcName = " authenticate"
)

type TunnelConfig struct {
	ConnectionConfig *connection.Config
	OSArch           string
	ClientID         string
	CloseConnOnce    *sync.Once // Used to close connectedSignal no more than once
	EdgeAddrs        []string
	HAConnections    int
	IncidentLookup   IncidentLookup
	IsAutoupdated    bool
	IsFreeTunnel     bool
	LBPool           string
	Tags             []tunnelpogs.Tag
	Log              *zerolog.Logger
	LogTransport     *zerolog.Logger
	Observer         *connection.Observer
	ReportedVersion  string
	Retries          uint
	RunFromTerminal  bool

	NamedTunnel      *connection.NamedTunnelConfig
	ClassicTunnel    *connection.ClassicTunnelConfig
	MuxerConfig      *connection.MuxerConfig
	ProtocolSelector connection.ProtocolSelector
	EdgeTLSConfigs   map[connection.Protocol]*tls.Config
}

func (c *TunnelConfig) RegistrationOptions(connectionID uint8, OriginLocalIP string, uuid uuid.UUID) *tunnelpogs.RegistrationOptions {
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

func (c *TunnelConfig) ConnectionOptions(originLocalAddr string, numPreviousAttempts uint8) *tunnelpogs.ConnectionOptions {
	// attempt to parse out origin IP, but don't fail since it's informational field
	host, _, _ := net.SplitHostPort(originLocalAddr)
	originIP := net.ParseIP(host)

	return &tunnelpogs.ConnectionOptions{
		Client:              c.NamedTunnel.Client,
		OriginLocalIP:       originIP,
		ReplaceExisting:     c.ConnectionConfig.ReplaceExisting,
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
	connectedSignal *signal.Signal,
	reconnectCh chan ReconnectSignal,
	graceShutdownC <-chan struct{},
) error {
	s, err := NewSupervisor(config, reconnectCh, graceShutdownC)
	if err != nil {
		return err
	}
	return s.Run(ctx, connectedSignal)
}

func ServeTunnelLoop(
	ctx context.Context,
	credentialManager *reconnectCredentialManager,
	config *TunnelConfig,
	addr *net.TCPAddr,
	connIndex uint8,
	connectedSignal *signal.Signal,
	cloudflaredUUID uuid.UUID,
	reconnectCh chan ReconnectSignal,
	gracefulShutdownC <-chan struct{},
) error {
	haConnections.Inc()
	defer haConnections.Dec()

	connLog := config.Log.With().Uint8(connection.LogFieldConnIndex, connIndex).Logger()

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
			&connLog,
			credentialManager,
			config,
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
		connLog.Info().Msgf("Retrying connection in up to %s seconds", duration)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-gracefulShutdownC:
			return nil
		case <-protocolFallback.BackoffTimer():
			if !selectNextProtocol(&connLog, protocolFallback, config.ProtocolSelector) {
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
) bool {
	if protocolBackoff.ReachedMaxRetries() {
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
	connLog *zerolog.Logger,
	credentialManager *reconnectCredentialManager,
	config *TunnelConfig,
	addr *net.TCPAddr,
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

	edgeConn, err := edgediscovery.DialEdge(ctx, dialTimeout, config.EdgeTLSConfigs[protocol], addr)
	if err != nil {
		connLog.Err(err).Msg("Unable to establish connection with Cloudflare edge")
		return err, true
	}
	connectedFuse := &connectedFuse{
		fuse:    fuse,
		backoff: backoff,
	}

	if protocol == connection.HTTP2 {
		connOptions := config.ConnectionOptions(edgeConn.LocalAddr().String(), uint8(backoff.Retries()))
		err = ServeHTTP2(
			ctx,
			connLog,
			config,
			edgeConn,
			connOptions,
			connIndex,
			connectedFuse,
			reconnectCh,
			gracefulShutdownC,
		)
	} else {
		err = ServeH2mux(
			ctx,
			connLog,
			credentialManager,
			config,
			edgeConn,
			connIndex,
			connectedFuse,
			cloudflaredUUID,
			reconnectCh,
			gracefulShutdownC,
		)
	}

	if err != nil {
		switch err := err.(type) {
		case connection.DupConnRegisterTunnelError:
			connLog.Err(err).Msg("Unable to establish connection.")
			// don't retry this connection anymore, let supervisor pick a new address
			return err, false
		case connection.ServerRegisterTunnelError:
			connLog.Err(err).Msg("Register tunnel error from server side")
			// Don't send registration error return from server to Sentry. They are
			// logged on server side
			if incidents := config.IncidentLookup.ActiveIncidents(); len(incidents) > 0 {
				connLog.Error().Msg(activeIncidentsMsg(incidents))
			}
			return err.Cause, !err.Permanent
		case ReconnectSignal:
			connLog.Info().
				Uint8(connection.LogFieldConnIndex, connIndex).
				Msgf("Restarting connection due to reconnect signal in %s", err.Delay)
			err.DelayBeforeReconnect()
			return err, true
		default:
			if err == context.Canceled {
				connLog.Debug().Err(err).Msgf("Serve tunnel error")
				return err, false
			}
			connLog.Err(err).Msgf("Serve tunnel error")
			_, permanent := err.(unrecoverableError)
			return err, !permanent
		}
	}
	return nil, false
}

type unrecoverableError struct {
	err error
}

func (r unrecoverableError) Error() string {
	return r.err.Error()
}

func ServeH2mux(
	ctx context.Context,
	connLog *zerolog.Logger,
	credentialManager *reconnectCredentialManager,
	config *TunnelConfig,
	edgeConn net.Conn,
	connIndex uint8,
	connectedFuse *connectedFuse,
	cloudflaredUUID uuid.UUID,
	reconnectCh chan ReconnectSignal,
	gracefulShutdownC <-chan struct{},
) error {
	connLog.Debug().Msgf("Connecting via h2mux")
	// Returns error from parsing the origin URL or handshake errors
	handler, err, recoverable := connection.NewH2muxConnection(
		config.ConnectionConfig,
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
			connOptions := config.ConnectionOptions(edgeConn.LocalAddr().String(), uint8(connectedFuse.backoff.Retries()))
			return handler.ServeNamedTunnel(serveCtx, config.NamedTunnel, connOptions, connectedFuse)
		}
		registrationOptions := config.RegistrationOptions(connIndex, edgeConn.LocalAddr().String(), cloudflaredUUID)
		return handler.ServeClassicTunnel(serveCtx, config.ClassicTunnel, credentialManager, registrationOptions, connectedFuse)
	})

	errGroup.Go(func() error {
		return listenReconnect(serveCtx, reconnectCh, gracefulShutdownC)
	})

	return errGroup.Wait()
}

func ServeHTTP2(
	ctx context.Context,
	connLog *zerolog.Logger,
	config *TunnelConfig,
	tlsServerConn net.Conn,
	connOptions *tunnelpogs.ConnectionOptions,
	connIndex uint8,
	connectedFuse connection.ConnectedFuse,
	reconnectCh chan ReconnectSignal,
	gracefulShutdownC <-chan struct{},
) error {
	connLog.Debug().Msgf("Connecting via http2")
	h2conn := connection.NewHTTP2Connection(
		tlsServerConn,
		config.ConnectionConfig,
		config.NamedTunnel,
		connOptions,
		config.Observer,
		connIndex,
		connectedFuse,
		gracefulShutdownC,
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
