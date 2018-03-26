package origin

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/net/context"
)

const (
	// Waiting time before retrying a failed tunnel connection
	tunnelRetryDuration = time.Second * 10
	// SRV record resolution TTL
	resolveTTL = time.Hour
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
	var tunnelsWaiting []int
	backoff := BackoffHandler{MaxRetries: s.config.Retries, BaseTime: tunnelRetryDuration, RetryForever: true}
	var backoffTimer <-chan time.Time

	for {
		select {
		// Context cancelled
		case <-ctx.Done():
			for len(s.tunnelErrors) > 0 {
				<-s.tunnelErrors
			}

			return nil
		// startTunnel returned with error
		// (note that this may also be caused by context cancellation)
		case tunnelError := <-s.tunnelErrors:
			if tunnelError.err != nil {
				Log.WithError(tunnelError.err).Warn("Tunnel disconnected due to error")
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
				go s.startTunnel(ctx, index, s.newConnectedTunnelSignal(index))
			}
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
				Log.Debug("Service discovery refresh complete")
				s.edgeIPs = result.edgeIPs
			} else {
				Log.WithError(result.err).Error("Service discovery error")
			}
		}
	}
}

func (s *Supervisor) initialize(ctx context.Context, connectedSignal chan struct{}) error {
	edgeIPs, err := ResolveEdgeIPs(s.config.EdgeAddrs)
	if err != nil {
		Log.Infof("ResolveEdgeIPs err")
		return err
	}
	s.edgeIPs = edgeIPs
	if s.config.HAConnections > len(edgeIPs) {
		Log.Warnf("You requested %d HA connections but I can give you at most %d.", s.config.HAConnections, len(edgeIPs))
		s.config.HAConnections = len(edgeIPs)
	}
	s.lastResolve = time.Now()
	// check entitlement and version too old error before attempting to register more tunnels
	s.nextUnusedEdgeIP = s.config.HAConnections
	go s.startFirstTunnel(ctx, connectedSignal)
	select {
	case <-ctx.Done():
		<-s.tunnelErrors
		// Error can't be nil. A nil error signals that initialization succeed
		return fmt.Errorf("context was canceled")
	case tunnelError := <-s.tunnelErrors:
		return tunnelError.err
	case <-connectedSignal:
	}
	// At least one successful connection, so start the rest
	for i := 1; i < s.config.HAConnections; i++ {
		go s.startTunnel(ctx, i, make(chan struct{}))
		// TODO: Add artificial delay between HA connections to make sure all origins
		// are registered in LB pool. Temporary fix until we fix LB
		time.Sleep(time.Millisecond * 500)
	}
	return nil
}

// startTunnel starts the first tunnel connection. The resulting error will be sent on
// s.tunnelErrors. It will send a signal via connectedSignal if registration succeed
func (s *Supervisor) startFirstTunnel(ctx context.Context, connectedSignal chan struct{}) {
	err := ServeTunnelLoop(ctx, s.config, s.getEdgeIP(0), 0, connectedSignal)
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
		err = ServeTunnelLoop(ctx, s.config, s.getEdgeIP(0), 0, connectedSignal)
	}
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

func (s *Supervisor) unusedIPs() bool {
	return s.nextUnusedEdgeIP < len(s.edgeIPs)
}

func (s *Supervisor) replaceEdgeIP(badIPIndex int) {
	s.edgeIPs[badIPIndex] = s.edgeIPs[s.nextUnusedEdgeIP]
	s.nextUnusedEdgeIP++
}
