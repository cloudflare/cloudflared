package supervisor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lucas-clemente/quic-go"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/orchestration"
	"github.com/cloudflare/cloudflared/retry"
	"github.com/cloudflare/cloudflared/signal"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	// SRV and TXT record resolution TTL
	ResolveTTL = time.Hour
	// Waiting time before retrying a failed tunnel connection
	tunnelRetryDuration = time.Second * 10
	// Interval between registering new tunnels
	registrationInterval = time.Second

	subsystemRefreshAuth = "refresh_auth"
	// Maximum exponent for 'Authenticate' exponential backoff
	refreshAuthMaxBackoff = 10
	// Waiting time before retrying a failed 'Authenticate' connection
	refreshAuthRetryDuration = time.Second * 10
)

// Supervisor manages non-declarative tunnels. Establishes TCP connections with the edge, and
// reconnects them if they disconnect.
type Supervisor struct {
	cloudflaredUUID         uuid.UUID
	config                  *TunnelConfig
	orchestrator            *orchestration.Orchestrator
	edgeIPs                 *edgediscovery.Edge
	edgeTunnelServer        EdgeTunnelServer
	tunnelErrors            chan tunnelError
	tunnelsConnecting       map[int]chan struct{}
	tunnelsProtocolFallback map[int]*protocolFallback
	// nextConnectedIndex and nextConnectedSignal are used to wait for all
	// currently-connecting tunnels to finish connecting so we can reset backoff timer
	nextConnectedIndex  int
	nextConnectedSignal chan struct{}

	log          *ConnAwareLogger
	logTransport *zerolog.Logger

	reconnectCredentialManager *reconnectCredentialManager
	useReconnectToken          bool

	reconnectCh       chan ReconnectSignal
	gracefulShutdownC <-chan struct{}
}

var errEarlyShutdown = errors.New("shutdown started")

type tunnelError struct {
	index int
	err   error
}

func NewSupervisor(config *TunnelConfig, orchestrator *orchestration.Orchestrator, reconnectCh chan ReconnectSignal, gracefulShutdownC <-chan struct{}) (*Supervisor, error) {
	cloudflaredUUID, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("failed to generate cloudflared instance ID: %w", err)
	}

	isStaticEdge := len(config.EdgeAddrs) > 0

	var edgeIPs *edgediscovery.Edge
	if isStaticEdge { // static edge addresses
		edgeIPs, err = edgediscovery.StaticEdge(config.Log, config.EdgeAddrs)
	} else {
		edgeIPs, err = edgediscovery.ResolveEdge(config.Log, config.Region, config.EdgeIPVersion)
	}
	if err != nil {
		return nil, err
	}

	reconnectCredentialManager := newReconnectCredentialManager(connection.MetricsNamespace, connection.TunnelSubsystem, config.HAConnections)
	log := NewConnAwareLogger(config.Log, config.Observer)

	var edgeAddrHandler EdgeAddrHandler
	if isStaticEdge { // static edge addresses
		edgeAddrHandler = &IPAddrFallback{}
	} else if config.EdgeIPVersion == allregions.IPv6Only || config.EdgeIPVersion == allregions.Auto {
		edgeAddrHandler = &IPAddrFallback{}
	} else { // IPv4Only
		edgeAddrHandler = &DefaultAddrFallback{}
	}

	edgeTunnelServer := EdgeTunnelServer{
		config:            config,
		cloudflaredUUID:   cloudflaredUUID,
		orchestrator:      orchestrator,
		credentialManager: reconnectCredentialManager,
		edgeAddrs:         edgeIPs,
		edgeAddrHandler:   edgeAddrHandler,
		reconnectCh:       reconnectCh,
		gracefulShutdownC: gracefulShutdownC,
		connAwareLogger:   log,
	}

	useReconnectToken := false
	if config.ClassicTunnel != nil {
		useReconnectToken = config.ClassicTunnel.UseReconnectToken
	}

	return &Supervisor{
		cloudflaredUUID:            cloudflaredUUID,
		config:                     config,
		orchestrator:               orchestrator,
		edgeIPs:                    edgeIPs,
		edgeTunnelServer:           edgeTunnelServer,
		tunnelErrors:               make(chan tunnelError),
		tunnelsConnecting:          map[int]chan struct{}{},
		tunnelsProtocolFallback:    map[int]*protocolFallback{},
		log:                        log,
		logTransport:               config.LogTransport,
		reconnectCredentialManager: reconnectCredentialManager,
		useReconnectToken:          useReconnectToken,
		reconnectCh:                reconnectCh,
		gracefulShutdownC:          gracefulShutdownC,
	}, nil
}

func (s *Supervisor) Run(
	ctx context.Context,
	connectedSignal *signal.Signal,
) error {
	if err := s.initialize(ctx, connectedSignal); err != nil {
		if err == errEarlyShutdown {
			return nil
		}
		return err
	}
	var tunnelsWaiting []int
	tunnelsActive := s.config.HAConnections

	backoff := retry.BackoffHandler{MaxRetries: s.config.Retries, BaseTime: tunnelRetryDuration, RetryForever: true}
	var backoffTimer <-chan time.Time

	refreshAuthBackoff := &retry.BackoffHandler{MaxRetries: refreshAuthMaxBackoff, BaseTime: refreshAuthRetryDuration, RetryForever: true}
	var refreshAuthBackoffTimer <-chan time.Time

	if s.useReconnectToken {
		if timer, err := s.reconnectCredentialManager.RefreshAuth(ctx, refreshAuthBackoff, s.authenticate); err == nil {
			refreshAuthBackoffTimer = timer
		} else {
			s.log.Logger().Err(err).
				Dur("refreshAuthRetryDuration", refreshAuthRetryDuration).
				Msgf("supervisor: initial refreshAuth failed, retrying in %v", refreshAuthRetryDuration)
			refreshAuthBackoffTimer = time.After(refreshAuthRetryDuration)
		}
	}

	shuttingDown := false
	for {
		select {
		// Context cancelled
		case <-ctx.Done():
			for tunnelsActive > 0 {
				<-s.tunnelErrors
				tunnelsActive--
			}
			return nil
		// startTunnel completed with a response
		// (note that this may also be caused by context cancellation)
		case tunnelError := <-s.tunnelErrors:
			tunnelsActive--
			if tunnelError.err != nil && !shuttingDown {
				switch tunnelError.err.(type) {
				case ReconnectSignal:
					// For tunnels that closed with reconnect signal, we reconnect immediately
					go s.startTunnel(ctx, tunnelError.index, s.newConnectedTunnelSignal(tunnelError.index))
					tunnelsActive++
					continue
				}
				// Make sure we don't continue if there is no more fallback allowed
				if _, retry := s.tunnelsProtocolFallback[tunnelError.index].GetMaxBackoffDuration(ctx); !retry {
					continue
				}
				s.log.ConnAwareLogger().Err(tunnelError.err).Int(connection.LogFieldConnIndex, tunnelError.index).Msg("Connection terminated")
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
		// Time to call Authenticate
		case <-refreshAuthBackoffTimer:
			newTimer, err := s.reconnectCredentialManager.RefreshAuth(ctx, refreshAuthBackoff, s.authenticate)
			if err != nil {
				s.log.Logger().Err(err).Msg("supervisor: Authentication failed")
				// Permanent failure. Leave the `select` without setting the
				// channel to be non-null, so we'll never hit this case of the `select` again.
				continue
			}
			refreshAuthBackoffTimer = newTimer
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

// Returns nil if initialization succeeded, else the initialization error.
// Attempts here will be made to connect one tunnel, if successful, it will
// connect the available tunnels up to config.HAConnections.
func (s *Supervisor) initialize(
	ctx context.Context,
	connectedSignal *signal.Signal,
) error {
	availableAddrs := s.edgeIPs.AvailableAddrs()
	if s.config.HAConnections > availableAddrs {
		s.log.Logger().Info().Msgf("You requested %d HA connections but I can give you at most %d.", s.config.HAConnections, availableAddrs)
		s.config.HAConnections = availableAddrs
	}
	s.tunnelsProtocolFallback[0] = &protocolFallback{
		retry.BackoffHandler{MaxRetries: s.config.Retries},
		s.config.ProtocolSelector.Current(),
		false,
	}

	go s.startFirstTunnel(ctx, connectedSignal)

	// Wait for response from first tunnel before proceeding to attempt other HA edge tunnels
	select {
	case <-ctx.Done():
		<-s.tunnelErrors
		return ctx.Err()
	case tunnelError := <-s.tunnelErrors:
		return tunnelError.err
	case <-s.gracefulShutdownC:
		return errEarlyShutdown
	case <-connectedSignal.Wait():
	}

	// At least one successful connection, so start the rest
	for i := 1; i < s.config.HAConnections; i++ {
		s.tunnelsProtocolFallback[i] = &protocolFallback{
			retry.BackoffHandler{MaxRetries: s.config.Retries},
			s.config.ProtocolSelector.Current(),
			false,
		}
		ch := signal.New(make(chan struct{}))
		go s.startTunnel(ctx, i, ch)
		time.Sleep(registrationInterval)
	}
	return nil
}

// startTunnel starts the first tunnel connection. The resulting error will be sent on
// s.tunnelErrors. It will send a signal via connectedSignal if registration succeed
func (s *Supervisor) startFirstTunnel(
	ctx context.Context,
	connectedSignal *signal.Signal,
) {
	var (
		err error
	)
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
		// Make sure we don't continue if there is no more fallback allowed
		if _, retry := s.tunnelsProtocolFallback[firstConnIndex].GetMaxBackoffDuration(ctx); !retry {
			return
		}
		// Try again for Unauthorized errors because we hope them to be
		// transient due to edge propagation lag on new Tunnels.
		if strings.Contains(err.Error(), "Unauthorized") {
			continue
		}
		switch err.(type) {
		case edgediscovery.ErrNoAddressesLeft:
			// If your provided addresses are not available, we will keep trying regardless.
			if !isStaticEdge {
				return
			}
		case connection.DupConnRegisterTunnelError,
			*quic.IdleTimeoutError,
			edgediscovery.DialError,
			*connection.EdgeQuicDialError:
			// Try again for these types of errors
		default:
			// Uncaught errors should bail startup
			return
		}
	}
}

// startTunnel starts a new tunnel connection. The resulting error will be sent on
// s.tunnelError as this is expected to run in a goroutine.
func (s *Supervisor) startTunnel(
	ctx context.Context,
	index int,
	connectedSignal *signal.Signal,
) {
	var (
		err error
	)
	defer func() {
		s.tunnelErrors <- tunnelError{index: index, err: err}
	}()

	err = s.edgeTunnelServer.Serve(ctx, uint8(index), s.tunnelsProtocolFallback[index], connectedSignal)
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

func (s *Supervisor) unusedIPs() bool {
	return s.edgeIPs.AvailableAddrs() > s.config.HAConnections
}

func (s *Supervisor) authenticate(ctx context.Context, numPreviousAttempts int) (tunnelpogs.AuthOutcome, error) {
	arbitraryEdgeIP, err := s.edgeIPs.GetAddrForRPC()
	if err != nil {
		return nil, err
	}

	edgeConn, err := edgediscovery.DialEdge(ctx, dialTimeout, s.config.EdgeTLSConfigs[connection.H2mux], arbitraryEdgeIP.TCP)
	if err != nil {
		return nil, err
	}
	defer edgeConn.Close()

	handler := h2mux.MuxedStreamFunc(func(*h2mux.MuxedStream) error {
		// This callback is invoked by h2mux when the edge initiates a stream.
		return nil // noop
	})
	muxerConfig := s.config.MuxerConfig.H2MuxerConfig(handler, s.logTransport)
	muxer, err := h2mux.Handshake(edgeConn, edgeConn, *muxerConfig, h2mux.ActiveStreams)
	if err != nil {
		return nil, err
	}
	go muxer.Serve(ctx)
	defer func() {
		// If we don't wait for the muxer shutdown here, edgeConn.Close() runs before the muxer connections are done,
		// and the user sees log noise: "error writing data", "connection closed unexpectedly"
		<-muxer.Shutdown()
	}()

	stream, err := muxer.OpenRPCStream(ctx)
	if err != nil {
		return nil, err
	}
	rpcClient := connection.NewTunnelServerClient(ctx, stream, s.log.Logger())
	defer rpcClient.Close()

	const arbitraryConnectionID = uint8(0)
	registrationOptions := s.config.registrationOptions(arbitraryConnectionID, edgeConn.LocalAddr().String(), s.cloudflaredUUID)
	registrationOptions.NumPreviousAttempts = uint8(numPreviousAttempts)
	return rpcClient.Authenticate(ctx, s.config.ClassicTunnel, registrationOptions)
}
