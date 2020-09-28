package origin

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/google/uuid"

	"github.com/cloudflare/cloudflared/buffer"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/signal"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

const (
	// Waiting time before retrying a failed tunnel connection
	tunnelRetryDuration = time.Second * 10
	// SRV record resolution TTL
	resolveTTL = time.Hour
	// Interval between registering new tunnels
	registrationInterval = time.Second

	subsystemRefreshAuth = "refresh_auth"
	// Maximum exponent for 'Authenticate' exponential backoff
	refreshAuthMaxBackoff = 10
	// Waiting time before retrying a failed 'Authenticate' connection
	refreshAuthRetryDuration = time.Second * 10
	// Maximum time to make an Authenticate RPC
	authTokenTimeout = time.Second * 30
)

var (
	errEventDigestUnset = errors.New("event digest unset")
)

// Supervisor manages non-declarative tunnels. Establishes TCP connections with the edge, and
// reconnects them if they disconnect.
type Supervisor struct {
	cloudflaredUUID   uuid.UUID
	config            *TunnelConfig
	edgeIPs           *edgediscovery.Edge
	lastResolve       time.Time
	resolverC         chan resolveResult
	tunnelErrors      chan tunnelError
	tunnelsConnecting map[int]chan struct{}
	// nextConnectedIndex and nextConnectedSignal are used to wait for all
	// currently-connecting tunnels to finish connecting so we can reset backoff timer
	nextConnectedIndex  int
	nextConnectedSignal chan struct{}

	logger logger.Service

	reconnectCredentialManager *reconnectCredentialManager

	bufferPool *buffer.Pool
}

type resolveResult struct {
	err error
}

type tunnelError struct {
	index int
	addr  *net.TCPAddr
	err   error
}

func NewSupervisor(config *TunnelConfig, cloudflaredUUID uuid.UUID) (*Supervisor, error) {
	var (
		edgeIPs *edgediscovery.Edge
		err     error
	)
	if len(config.EdgeAddrs) > 0 {
		edgeIPs, err = edgediscovery.StaticEdge(config.Logger, config.EdgeAddrs)
	} else {
		edgeIPs, err = edgediscovery.ResolveEdge(config.Logger)
	}
	if err != nil {
		return nil, err
	}

	return &Supervisor{
		cloudflaredUUID:            cloudflaredUUID,
		config:                     config,
		edgeIPs:                    edgeIPs,
		tunnelErrors:               make(chan tunnelError),
		tunnelsConnecting:          map[int]chan struct{}{},
		logger:                     config.Logger,
		reconnectCredentialManager: newReconnectCredentialManager(metricsNamespace, tunnelSubsystem, config.HAConnections),
		bufferPool:                 buffer.NewPool(512 * 1024),
	}, nil
}

func (s *Supervisor) Run(ctx context.Context, connectedSignal *signal.Signal, reconnectCh chan ReconnectSignal) error {
	logger := s.config.Logger
	if err := s.initialize(ctx, connectedSignal, reconnectCh); err != nil {
		return err
	}
	var tunnelsWaiting []int
	tunnelsActive := s.config.HAConnections

	backoff := BackoffHandler{MaxRetries: s.config.Retries, BaseTime: tunnelRetryDuration, RetryForever: true}
	var backoffTimer <-chan time.Time

	refreshAuthBackoff := &BackoffHandler{MaxRetries: refreshAuthMaxBackoff, BaseTime: refreshAuthRetryDuration, RetryForever: true}
	var refreshAuthBackoffTimer <-chan time.Time

	if s.config.UseReconnectToken {
		if timer, err := s.reconnectCredentialManager.RefreshAuth(ctx, refreshAuthBackoff, s.authenticate); err == nil {
			refreshAuthBackoffTimer = timer
		} else {
			logger.Errorf("supervisor: initial refreshAuth failed, retrying in %v: %s", refreshAuthRetryDuration, err)
			refreshAuthBackoffTimer = time.After(refreshAuthRetryDuration)
		}
	}

	for {
		select {
		// Context cancelled
		case <-ctx.Done():
			for tunnelsActive > 0 {
				<-s.tunnelErrors
				tunnelsActive--
			}
			return nil
		// startTunnel returned with error
		// (note that this may also be caused by context cancellation)
		case tunnelError := <-s.tunnelErrors:
			tunnelsActive--
			if tunnelError.err != nil {
				logger.Infof("supervisor: Tunnel disconnected due to error: %s", tunnelError.err)
				tunnelsWaiting = append(tunnelsWaiting, tunnelError.index)
				s.waitForNextTunnel(tunnelError.index)

				if backoffTimer == nil {
					backoffTimer = backoff.BackoffTimer()
				}

				// Previously we'd mark the edge address as bad here, but now we'll just silently use
				// another.
			}
		// Backoff was set and its timer expired
		case <-backoffTimer:
			backoffTimer = nil
			for _, index := range tunnelsWaiting {
				go s.startTunnel(ctx, index, s.newConnectedTunnelSignal(index), reconnectCh)
			}
			tunnelsActive += len(tunnelsWaiting)
			tunnelsWaiting = nil
		// Time to call Authenticate
		case <-refreshAuthBackoffTimer:
			newTimer, err := s.reconnectCredentialManager.RefreshAuth(ctx, refreshAuthBackoff, s.authenticate)
			if err != nil {
				logger.Errorf("supervisor: Authentication failed: %s", err)
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
		// DNS resolution returned
		case result := <-s.resolverC:
			s.lastResolve = time.Now()
			s.resolverC = nil
			if result.err == nil {
				logger.Debug("supervisor: Service discovery refresh complete")
			} else {
				logger.Errorf("supervisor: Service discovery error: %s", result.err)
			}
		}
	}
}

// Returns nil if initialization succeeded, else the initialization error.
func (s *Supervisor) initialize(ctx context.Context, connectedSignal *signal.Signal, reconnectCh chan ReconnectSignal) error {
	logger := s.logger

	s.lastResolve = time.Now()
	availableAddrs := int(s.edgeIPs.AvailableAddrs())
	if s.config.HAConnections > availableAddrs {
		logger.Infof("You requested %d HA connections but I can give you at most %d.", s.config.HAConnections, availableAddrs)
		s.config.HAConnections = availableAddrs
	}

	go s.startFirstTunnel(ctx, connectedSignal, reconnectCh)
	select {
	case <-ctx.Done():
		<-s.tunnelErrors
		return ctx.Err()
	case tunnelError := <-s.tunnelErrors:
		return tunnelError.err
	case <-connectedSignal.Wait():
	}
	// At least one successful connection, so start the rest
	for i := 1; i < s.config.HAConnections; i++ {
		ch := signal.New(make(chan struct{}))
		go s.startTunnel(ctx, i, ch, reconnectCh)
		time.Sleep(registrationInterval)
	}
	return nil
}

// startTunnel starts the first tunnel connection. The resulting error will be sent on
// s.tunnelErrors. It will send a signal via connectedSignal if registration succeed
func (s *Supervisor) startFirstTunnel(ctx context.Context, connectedSignal *signal.Signal, reconnectCh chan ReconnectSignal) {
	var (
		addr *net.TCPAddr
		err  error
	)
	const firstConnIndex = 0
	defer func() {
		s.tunnelErrors <- tunnelError{index: firstConnIndex, addr: addr, err: err}
	}()

	addr, err = s.edgeIPs.GetAddr(firstConnIndex)
	if err != nil {
		return
	}

	err = ServeTunnelLoop(ctx, s.reconnectCredentialManager, s.config, addr, firstConnIndex, connectedSignal, s.cloudflaredUUID, s.bufferPool, reconnectCh)
	// If the first tunnel disconnects, keep restarting it.
	edgeErrors := 0
	for s.unusedIPs() {
		if ctx.Err() != nil {
			return
		}
		switch err.(type) {
		case nil:
			return
		// try the next address if it was a dialError(network problem) or
		// dupConnRegisterTunnelError
		case connection.DialError, dupConnRegisterTunnelError:
			edgeErrors++
		default:
			return
		}
		if edgeErrors >= 2 {
			addr, err = s.edgeIPs.GetDifferentAddr(firstConnIndex)
			if err != nil {
				return
			}
		}
		err = ServeTunnelLoop(ctx, s.reconnectCredentialManager, s.config, addr, firstConnIndex, connectedSignal, s.cloudflaredUUID, s.bufferPool, reconnectCh)
	}
}

// startTunnel starts a new tunnel connection. The resulting error will be sent on
// s.tunnelErrors.
func (s *Supervisor) startTunnel(ctx context.Context, index int, connectedSignal *signal.Signal, reconnectCh chan ReconnectSignal) {
	var (
		addr *net.TCPAddr
		err  error
	)
	defer func() {
		s.tunnelErrors <- tunnelError{index: index, addr: addr, err: err}
	}()

	addr, err = s.edgeIPs.GetDifferentAddr(index)
	if err != nil {
		return
	}
	err = ServeTunnelLoop(ctx, s.reconnectCredentialManager, s.config, addr, uint8(index), connectedSignal, s.cloudflaredUUID, s.bufferPool, reconnectCh)
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

	edgeConn, err := connection.DialEdge(ctx, dialTimeout, s.config.TlsConfig, arbitraryEdgeIP)
	if err != nil {
		return nil, err
	}
	defer edgeConn.Close()

	handler := h2mux.MuxedStreamFunc(func(*h2mux.MuxedStream) error {
		// This callback is invoked by h2mux when the edge initiates a stream.
		return nil // noop
	})
	muxerConfig := s.config.muxerConfig(handler)
	muxer, err := h2mux.Handshake(edgeConn, edgeConn, muxerConfig, s.config.Metrics.activeStreams)
	if err != nil {
		return nil, err
	}
	go muxer.Serve(ctx)
	defer func() {
		// If we don't wait for the muxer shutdown here, edgeConn.Close() runs before the muxer connections are done,
		// and the user sees log noise: "error writing data", "connection closed unexpectedly"
		<-muxer.Shutdown()
	}()

	rpcClient, err := newTunnelRPCClient(ctx, muxer, s.config, authenticate)
	if err != nil {
		return nil, err
	}
	defer rpcClient.Close()

	const arbitraryConnectionID = uint8(0)
	registrationOptions := s.config.RegistrationOptions(arbitraryConnectionID, edgeConn.LocalAddr().String(), s.cloudflaredUUID)
	registrationOptions.NumPreviousAttempts = uint8(numPreviousAttempts)
	authResponse, err := rpcClient.Authenticate(
		ctx,
		s.config.OriginCert,
		s.config.Hostname,
		registrationOptions,
	)
	if err != nil {
		return nil, err
	}
	return authResponse.Outcome(), nil
}
