package origin

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/signal"

	"github.com/google/uuid"
)

const (
	// Waiting time before retrying a failed tunnel connection
	tunnelRetryDuration = time.Second * 10
	// SRV record resolution TTL
	resolveTTL = time.Hour
	// Interval between registering new tunnels
	registrationInterval = time.Second
)

type Supervisor struct {
	config  *TunnelConfig
	edgeIPs []*net.TCPAddr
	// nextUnusedEdgeIP is the index of the next addr k edgeIPs to try
	nextUnusedEdgeIP  int
	lastResolve       time.Time
	resolverC         chan resolveResult
	tunnelErrors      chan tunnelError
	tunnelsConnecting map[int]chan struct{}
	// nextConnectedIndex and nextConnectedSignal are used to wait for all
	// currently-connecting tunnels to finish connecting so we can reset backoff timer
	nextConnectedIndex  int
	nextConnectedSignal chan struct{}

	logger *logrus.Entry
}

type resolveResult struct {
	edgeIPs []*net.TCPAddr
	err     error
}

type tunnelError struct {
	index int
	err   error
}

func NewSupervisor(config *TunnelConfig) *Supervisor {
	return &Supervisor{
		config:            config,
		tunnelErrors:      make(chan tunnelError),
		tunnelsConnecting: map[int]chan struct{}{},
		logger:            config.Logger.WithField("subsystem", "supervisor"),
	}
}

func (s *Supervisor) Run(ctx context.Context, connectedSignal *signal.Signal, u uuid.UUID) error {
	logger := s.config.Logger
	if err := s.initialize(ctx, connectedSignal, u); err != nil {
		return err
	}
	var tunnelsWaiting []int
	backoff := BackoffHandler{MaxRetries: s.config.Retries, BaseTime: tunnelRetryDuration, RetryForever: true}
	var backoffTimer <-chan time.Time
	tunnelsActive := s.config.HAConnections

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
				logger.WithError(tunnelError.err).Warn("Tunnel disconnected due to error")
				tunnelsWaiting = append(tunnelsWaiting, tunnelError.index)
				s.waitForNextTunnel(tunnelError.index)

				if backoffTimer == nil {
					backoffTimer = backoff.BackoffTimer()
				}

				// If the error is a dial error, the problem is likely to be network related
				// try another addr before refreshing since we are likely to get back the
				// same IPs in the same order. Same problem with duplicate connection error.
				if s.unusedIPs() {
					s.replaceEdgeIP(tunnelError.index)
				} else {
					s.refreshEdgeIPs()
				}
			}
		// Backoff was set and its timer expired
		case <-backoffTimer:
			backoffTimer = nil
			for _, index := range tunnelsWaiting {
				go s.startTunnel(ctx, index, s.newConnectedTunnelSignal(index), u)
			}
			tunnelsActive += len(tunnelsWaiting)
			tunnelsWaiting = nil
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
				logger.Debug("Service discovery refresh complete")
				s.edgeIPs = result.edgeIPs
			} else {
				logger.WithError(result.err).Error("Service discovery error")
			}
		}
	}
}

func (s *Supervisor) initialize(ctx context.Context, connectedSignal *signal.Signal, u uuid.UUID) error {
	logger := s.logger

	edgeIPs, err := s.resolveEdgeIPs()

	if err != nil {
		logger.Infof("ResolveEdgeIPs err")
		return err
	}
	s.edgeIPs = edgeIPs
	if s.config.HAConnections > len(edgeIPs) {
		logger.Warnf("You requested %d HA connections but I can give you at most %d.", s.config.HAConnections, len(edgeIPs))
		s.config.HAConnections = len(edgeIPs)
	}
	s.lastResolve = time.Now()
	// check entitlement and version too old error before attempting to register more tunnels
	s.nextUnusedEdgeIP = s.config.HAConnections
	go s.startFirstTunnel(ctx, connectedSignal, u)
	select {
	case <-ctx.Done():
		<-s.tunnelErrors
		// Error can't be nil. A nil error signals that initialization succeed
		return fmt.Errorf("context was canceled")
	case tunnelError := <-s.tunnelErrors:
		return tunnelError.err
	case <-connectedSignal.Wait():
	}
	// At least one successful connection, so start the rest
	for i := 1; i < s.config.HAConnections; i++ {
		ch := signal.New(make(chan struct{}))
		go s.startTunnel(ctx, i, ch, u)
		time.Sleep(registrationInterval)
	}
	return nil
}

// startTunnel starts the first tunnel connection. The resulting error will be sent on
// s.tunnelErrors. It will send a signal via connectedSignal if registration succeed
func (s *Supervisor) startFirstTunnel(ctx context.Context, connectedSignal *signal.Signal, u uuid.UUID) {
	err := ServeTunnelLoop(ctx, s.config, s.getEdgeIP(0), 0, connectedSignal, u)
	defer func() {
		s.tunnelErrors <- tunnelError{index: 0, err: err}
	}()

	for s.unusedIPs() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		switch err.(type) {
		case nil:
			return
		// try the next address if it was a dialError(network problem) or
		// dupConnRegisterTunnelError
		case dialError, dupConnRegisterTunnelError:
			s.replaceEdgeIP(0)
		default:
			return
		}
		err = ServeTunnelLoop(ctx, s.config, s.getEdgeIP(0), 0, connectedSignal, u)
	}
}

// startTunnel starts a new tunnel connection. The resulting error will be sent on
// s.tunnelErrors.
func (s *Supervisor) startTunnel(ctx context.Context, index int, connectedSignal *signal.Signal, u uuid.UUID) {
	err := ServeTunnelLoop(ctx, s.config, s.getEdgeIP(index), uint8(index), connectedSignal, u)
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

func (s *Supervisor) getEdgeIP(index int) *net.TCPAddr {
	return s.edgeIPs[index%len(s.edgeIPs)]
}

func (s *Supervisor) resolveEdgeIPs() ([]*net.TCPAddr, error) {
	// If --edge is specfied, resolve edge server addresses
	if len(s.config.EdgeAddrs) > 0 {
		return connection.ResolveAddrs(s.config.EdgeAddrs)
	}
	// Otherwise lookup edge server addresses through service discovery
	return connection.EdgeDiscovery(s.logger)
}

func (s *Supervisor) refreshEdgeIPs() {
	if s.resolverC != nil {
		return
	}
	if time.Since(s.lastResolve) < resolveTTL {
		return
	}
	s.resolverC = make(chan resolveResult)
	go func() {
		edgeIPs, err := s.resolveEdgeIPs()
		s.resolverC <- resolveResult{edgeIPs: edgeIPs, err: err}
	}()
}

func (s *Supervisor) unusedIPs() bool {
	return s.nextUnusedEdgeIP < len(s.edgeIPs)
}

func (s *Supervisor) replaceEdgeIP(badIPIndex int) {
	s.edgeIPs[badIPIndex] = s.edgeIPs[s.nextUnusedEdgeIP]
	s.nextUnusedEdgeIP++
}
