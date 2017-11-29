package origin

import (
	"fmt"
	"net"
	"time"

	log "github.com/Sirupsen/logrus"
	"golang.org/x/net/context"
)

const (
	// Waiting time before retrying a failed tunnel connection
	tunnelRetryDuration = time.Minute
	// Limit on the exponential backoff time period. (2^5 = 32 minutes)
	tunnelRetryLimit = 5
	// SRV record resolution TTL
	resolveTTL = time.Hour
)

type Supervisor struct {
	config              *TunnelConfig
	edgeIPs             []*net.TCPAddr
	lastResolve         time.Time
	resolverC           chan resolveResult
	tunnelErrors        chan tunnelError
	tunnelsConnecting   map[int]chan struct{}
	nextConnectedIndex  int
	nextConnectedSignal chan struct{}
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
	}
}

func (s *Supervisor) Run(ctx context.Context, connectedSignal chan struct{}) error {
	if err := s.initialize(ctx, connectedSignal); err != nil {
		return err
	}
	tunnelsActive := s.config.HAConnections
	tunnelsWaiting := []int{}
	backoff := BackoffHandler{MaxRetries: tunnelRetryLimit, BaseTime: tunnelRetryDuration, RetryForever: true}
	var backoffTimer <-chan time.Time
	for tunnelsActive > 0 {
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
				log.WithError(tunnelError.err).Warn("Tunnel disconnected due to error")
				tunnelsWaiting = append(tunnelsWaiting, tunnelError.index)
				s.waitForNextTunnel(tunnelError.index)
				if backoffTimer != nil {
					backoffTimer = backoff.BackoffTimer()
				}
				s.refreshEdgeIPs()
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
		// DNS resolution returned
		case result := <-s.resolverC:
			s.lastResolve = time.Now()
			s.resolverC = nil
			if result.err == nil {
				log.Debug("Service discovery refresh complete")
				s.edgeIPs = result.edgeIPs
			} else {
				log.WithError(result.err).Error("Service discovery error")
			}
		}
	}
	return fmt.Errorf("All tunnels terminated")
}

func (s *Supervisor) initialize(ctx context.Context, connectedSignal chan struct{}) error {
	edgeIPs, err := ResolveEdgeIPs(s.config.EdgeAddrs)
	if err != nil {
		return err
	}
	s.edgeIPs = edgeIPs
	s.lastResolve = time.Now()
	go s.startTunnel(ctx, 0, connectedSignal)
	select {
	case <-ctx.Done():
		<-s.tunnelErrors
		return nil
	case tunnelError := <-s.tunnelErrors:
		return tunnelError.err
	case <-connectedSignal:
	}
	// At least one successful connection, so start the rest
	for i := 1; i < s.config.HAConnections; i++ {
		go s.startTunnel(ctx, i, make(chan struct{}))
	}
	return nil
}

// startTunnel starts a new tunnel connection. The resulting error will be sent on
// s.tunnelErrors.
func (s *Supervisor) startTunnel(ctx context.Context, index int, connectedSignal chan struct{}) {
	err := ServeTunnelLoop(ctx, s.config, s.getEdgeIP(index), uint8(index), connectedSignal)
	s.tunnelErrors <- tunnelError{index: index, err: err}
}

func (s *Supervisor) newConnectedTunnelSignal(index int) chan struct{} {
	signal := make(chan struct{})
	s.tunnelsConnecting[index] = signal
	s.nextConnectedSignal = signal
	s.nextConnectedIndex = index
	return signal
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

func (s *Supervisor) refreshEdgeIPs() {
	if s.resolverC != nil {
		return
	}
	if time.Since(s.lastResolve) < resolveTTL {
		return
	}
	s.resolverC = make(chan resolveResult)
	go func() {
		edgeIPs, err := ResolveEdgeIPs(s.config.EdgeAddrs)
		s.resolverC <- resolveResult{edgeIPs: edgeIPs, err: err}
	}()
}
