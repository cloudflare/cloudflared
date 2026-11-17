package supervisor

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/quic-go/quic-go"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/orchestration"
	v3 "github.com/cloudflare/cloudflared/quic/v3"
	"github.com/cloudflare/cloudflared/retry"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

const (
	// Waiting time before retrying a failed tunnel connection
	tunnelRetryDuration = time.Second * 10
	// Interval between registering new tunnels
	registrationInterval = time.Second
)

// Supervisor manages non-declarative tunnels. Establishes TCP connections with the edge, and
// reconnects them if they disconnect.
type Supervisor struct {
	config                  *TunnelConfig
	orchestrator            *orchestration.Orchestrator
	edgeIPs                 *edgediscovery.Edge
	edgeTunnelServer        TunnelServer
	tunnelErrors            chan tunnelError
	tunnelsConnecting       map[int]chan struct{}
	tunnelsProtocolFallback map[int]*protocolFallback
	// nextConnectedIndex and nextConnectedSignal are used to wait for all
	// currently-connecting tunnels to finish connecting so we can reset backoff timer
	nextConnectedIndex  int
	nextConnectedSignal chan struct{}

	log *ConnAwareLogger

	gracefulShutdownC <-chan struct{}
}

var errEarlyShutdown = errors.New("shutdown started")

type tunnelError struct {
	index int
	err   error
}

func NewSupervisor(config *TunnelConfig, orchestrator *orchestration.Orchestrator, gracefulShutdownC <-chan struct{}) (*Supervisor, error) {
	isStaticEdge := len(config.EdgeAddrs) > 0

	var err error
	var edgeIPs *edgediscovery.Edge
	if isStaticEdge { // static edge addresses
		edgeIPs, err = edgediscovery.StaticEdge(config.Log, config.EdgeAddrs)
	} else {
		edgeIPs, err = edgediscovery.ResolveEdge(config.Log, config.Region, config.EdgeIPVersion)
	}
	if err != nil {
		return nil, err
	}
	// Cap the group size only once. Edge addresses remain assigned across
	// restarts, so AvailableAddrs excludes addresses used by the previous group.
	availableAddrs := edgeIPs.AvailableAddrs()
	if config.HAConnections > availableAddrs {
		config.Log.Info().Msgf("%d HA connections were requested but the maximum available is %d.", config.HAConnections, availableAddrs)
		config.HAConnections = availableAddrs
	}

	tracker := tunnelstate.NewConnTracker(config.Log)
	log := NewConnAwareLogger(config.Log, tracker, config.Observer)

	edgeAddrHandler := NewIPAddrFallback(config.MaxEdgeAddrRetries)
	edgeBindAddr := config.EdgeBindAddr

	datagramMetrics := v3.NewMetrics(prometheus.DefaultRegisterer)

	sessionManager := v3.NewSessionManager(datagramMetrics, config.Log, config.OriginDialerService, orchestrator.GetFlowLimiter())

	edgeTunnelServer := EdgeTunnelServer{
		config:            config,
		orchestrator:      orchestrator,
		sessionManager:    sessionManager,
		datagramMetrics:   datagramMetrics,
		edgeAddrs:         edgeIPs,
		edgeAddrHandler:   edgeAddrHandler,
		edgeBindAddr:      edgeBindAddr,
		tracker:           tracker,
		gracefulShutdownC: gracefulShutdownC,
		connAwareLogger:   log,
	}

	return &Supervisor{
		config:                  config,
		orchestrator:            orchestrator,
		edgeIPs:                 edgeIPs,
		edgeTunnelServer:        &edgeTunnelServer,
		tunnelErrors:            make(chan tunnelError),
		tunnelsConnecting:       map[int]chan struct{}{},
		tunnelsProtocolFallback: map[int]*protocolFallback{},
		log:                     log,
		gracefulShutdownC:       gracefulShutdownC,
	}, nil
}

func (s *Supervisor) Run(
	ctx context.Context,
	connectedSignal *signal.Signal,
) error {
	if s.config.ICMPRouterServer != nil {
		go func() {
			if err := s.config.ICMPRouterServer.Serve(ctx); err != nil && ctx.Err() == nil {
				s.log.Logger().Err(err).Msg("icmp router terminated")
			}
		}()
	}

	// Setup DNS Resolver refresh
	go s.config.OriginDNSService.StartRefreshLoop(ctx)

	return s.runWithRegionChanges(ctx, connectedSignal)
}

// runWithRegionChanges runs the tunnel connections until shutdown. On an account
// region change, it waits for all connections to stop before reconnecting.
func (s *Supervisor) runWithRegionChanges(ctx context.Context, connectedSignal *signal.Signal) error {
	var nextRegionChange time.Time
	for {
		err := s.runConnections(ctx, connectedSignal)
		if ctx.Err() != nil {
			return nil
		}
		if !errors.Is(err, connection.ErrRegionChanged) {
			return err
		}
		// Handle the first change immediately, but rate-limit repeated changes.
		// The old HA group is already drained while we wait.
		select {
		case <-ctx.Done():
			return nil
		case <-s.gracefulShutdownC:
			return nil
		case <-time.After(time.Until(nextRegionChange)):
		}
		nextRegionChange = time.Now().Add(tunnelRetryDuration)
		s.log.Logger().Info().Msg("Account region changed; restarting tunnel connections")

		// runConnections has drained the old group, so no connection goroutine
		// can still use this map or notify the previous group's signal.
		s.tunnelsConnecting = make(map[int]chan struct{})
		s.nextConnectedSignal = nil
	}
}

// runConnections owns one HA connection group. Every exit cancels and drains
// that group before the supervisor can start another.
func (s *Supervisor) runConnections(ctx context.Context, connectedSignal *signal.Signal) error {
	ctx, cancel := context.WithCancel(ctx)
	tunnelsActive := 0
	defer func() {
		cancel()
		// Each started tunnel sends one result before exiting. Wait for all
		// outstanding results before reusing connection state in the next group.
		for ; tunnelsActive > 0; tunnelsActive-- {
			<-s.tunnelErrors
		}
	}()
	var err error
	tunnelsActive, err = s.initialize(ctx, connectedSignal)
	if err != nil {
		if err == errEarlyShutdown {
			return nil
		}
		s.log.Logger().Error().Err(err).Msg("initial tunnel connection failed")
		return err
	}
	var tunnelsWaiting []int

	backoff := retry.NewBackoff(s.config.Retries, tunnelRetryDuration, true)
	var backoffTimer <-chan time.Time

	shuttingDown := false
	for {
		select {
		// Context cancelled
		case <-ctx.Done():
			return nil
		// startTunnel completed with a response
		// (note that this may also be caused by context cancellation)
		case tunnelError := <-s.tunnelErrors:
			tunnelsActive--
			if !shuttingDown && errors.Is(tunnelError.err, connection.ErrRegionChanged) {
				// The deferred cleanup drains the entire group. The outer loop
				// handles the restart instead of retrying this connection alone.
				return connection.ErrRegionChanged
			}
			s.log.ConnAwareLogger().Err(tunnelError.err).Int(connection.LogFieldConnIndex, tunnelError.index).Msg("Connection terminated")
			if tunnelError.err != nil && !shuttingDown {
				// Make sure we don't continue if there is no more fallback allowed
				if _, retry := s.tunnelsProtocolFallback[tunnelError.index].GetMaxBackoffDuration(ctx); !retry {
					continue
				}
				tunnelsWaiting = append(tunnelsWaiting, tunnelError.index)
				s.waitForNextTunnel(tunnelError.index)

				if backoffTimer == nil {
					backoffTimer = backoff.BackoffTimer()
				}
			} else if tunnelsActive == 0 {
				s.log.ConnAwareLogger().Msg("no more connections active and exiting")
				// All connected tunnels exited gracefully, no more work to do
				return nil
			}
		// Backoff was set and its timer expired
		case <-backoffTimer:
			backoffTimer = nil
			for _, index := range tunnelsWaiting {
				go s.startTunnel(ctx, index, s.newConnectedTunnelSignal(index))
			}
			tunnelsActive += len(tunnelsWaiting)
			tunnelsWaiting = nil
		// Tunnel successfully connected
		case <-s.nextConnectedSignal:
			if !s.waitForNextTunnel(s.nextConnectedIndex) && len(tunnelsWaiting) == 0 {
				// No more tunnels outstanding, clear backoff timer
				backoff.SetGracePeriod()
			}
		case <-s.gracefulShutdownC:
			shuttingDown = true
		}
	}
}

// initialize connects the first tunnel, then starts the remaining HA connections.
// It returns the number of active tunnels still to be drained and any initialization error.
//
// Graceful shutdown leaves ctx active, so both the first-connection wait and HA
// registration delays must also watch gracefulShutdownC. This matters on initial
// startup and when a region change sends us through initialization again: the
// runConnections shutdown loop is not running yet. Returning errEarlyShutdown
// lets runConnections cancel and drain the started tunnels without waiting for
// initialization to finish or starting the remaining HA connections.
func (s *Supervisor) initialize(
	ctx context.Context,
	connectedSignal *signal.Signal,
) (int, error) {
	s.tunnelsProtocolFallback[0] = &protocolFallback{
		retry.NewBackoff(s.config.Retries, retry.DefaultBaseTime, true),
		s.config.ProtocolSelector.Current(),
		false,
	}

	// Each group must await its own first connection, even if the daemon's
	// one-time connected signal was already notified by an earlier group.
	firstConnected := signal.New(make(chan struct{}))
	go s.startFirstTunnel(ctx, firstConnected)

	// Wait for response from first tunnel before proceeding to attempt other HA edge tunnels
	select {
	case <-ctx.Done():
		return 1, ctx.Err()
	case tunnelError := <-s.tunnelErrors:
		// The first tunnel's result was already consumed, so nothing remains to drain.
		return 0, tunnelError.err
	case <-s.gracefulShutdownC:
		return 1, errEarlyShutdown
	case <-firstConnected.Wait():
		connectedSignal.Notify()
	}

	// At least one successful connection, so start the rest
	active := 1
	for i := 1; i < s.config.HAConnections; i++ {
		s.tunnelsProtocolFallback[i] = &protocolFallback{
			retry.NewBackoff(s.config.Retries, retry.DefaultBaseTime, true),
			// Set the protocol we know the first tunnel connected with.
			s.tunnelsProtocolFallback[0].protocol,
			false,
		}
		go s.startTunnel(ctx, i, s.newConnectedTunnelSignal(i))
		active++
		select {
		case <-ctx.Done():
			return active, ctx.Err()
		case <-s.gracefulShutdownC:
			return active, errEarlyShutdown
		case <-time.After(registrationInterval):
		}
	}
	return active, nil
}

// startTunnel starts the first tunnel connection. The resulting error will be sent on
// s.tunnelErrors. It will send a signal via connectedSignal if registration succeed
func (s *Supervisor) startFirstTunnel(
	ctx context.Context,
	connectedSignal *signal.Signal,
) {
	var err error
	const firstConnIndex = 0
	isStaticEdge := len(s.config.EdgeAddrs) > 0
	defer func() {
		s.tunnelErrors <- tunnelError{index: firstConnIndex, err: err}
	}()

	// If the first tunnel disconnects, keep restarting it.
	for {
		err = s.edgeTunnelServer.Serve(ctx, firstConnIndex, s.tunnelsProtocolFallback[firstConnIndex], connectedSignal)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			return
		}
		if errors.Is(err, connection.ErrRegionChanged) {
			// Restarting only the first connection would leave the rest of the
			// HA group using the old region. Let the supervisor restart them all.
			return
		}
		// Make sure we don't continue if there is no more fallback allowed
		if _, retry := s.tunnelsProtocolFallback[firstConnIndex].GetMaxBackoffDuration(ctx); !retry {
			return
		}
		// Try again for Unauthorized errors because we hope them to be
		// transient due to edge propagation lag on new Tunnels.
		if strings.Contains(err.Error(), "Unauthorized") {
			continue
		}
		if registrationErr, ok := errors.AsType[connection.ServerRegisterTunnelError](err); ok {
			if registrationErr.Permanent {
				return
			}
			continue
		}
		if _, ok := errors.AsType[edgediscovery.ErrNoAddressesLeft](err); ok {
			// If your provided addresses are not available, we will keep trying regardless.
			if !isStaticEdge {
				return
			}
			continue
		}
		if !isRetryableStartupError(err) {
			// Non-retryable errors should bail startup
			return
		}
	}
}

// isRetryableStartupError reports whether the first tunnel connection should keep
// retrying for the given error. Wrapped connection errors are classified by their inner type.
func isRetryableStartupError(err error) bool {
	if errors.Is(err, connection.ErrDuplicateConnection) {
		return true
	}
	if _, ok := errors.AsType[*quic.IdleTimeoutError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*quic.ApplicationError](err); ok {
		return true
	}
	if _, ok := errors.AsType[edgediscovery.DialError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*connection.EdgeQuicDialError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*connection.ControlStreamError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*connection.StreamListenerError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*connection.DatagramManagerError](err); ok {
		return true
	}
	return false
}

// startTunnel starts a new tunnel connection. The resulting error will be sent on
// s.tunnelError as this is expected to run in a goroutine.
func (s *Supervisor) startTunnel(
	ctx context.Context,
	index int,
	connectedSignal *signal.Signal,
) {
	// nolint: gosec
	err := s.edgeTunnelServer.Serve(ctx, uint8(index), s.tunnelsProtocolFallback[index], connectedSignal)
	s.tunnelErrors <- tunnelError{index: index, err: err}
}

func (s *Supervisor) newConnectedTunnelSignal(index int) *signal.Signal {
	sig := make(chan struct{})
	s.tunnelsConnecting[index] = sig
	s.nextConnectedSignal = sig
	s.nextConnectedIndex = index
	return signal.New(sig)
}

func (s *Supervisor) waitForNextTunnel(index int) bool {
	delete(s.tunnelsConnecting, index)
	s.nextConnectedSignal = nil
	for k, v := range s.tunnelsConnecting {
		s.nextConnectedIndex = k
		s.nextConnectedSignal = v
		return true
	}
	return false
}
