package main

import (
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/tunneldns"
	"github.com/sirupsen/logrus"
)

// ResolverServiceType is used to identify what kind of overwatch service this is
const ResolverServiceType = "resolver"

// ResolverService is used to wrap the tunneldns package's DNS over HTTP
// into a service model for the overwatch package.
// it also holds a reference to the config object that represents its state
type ResolverService struct {
	resolver config.DNSResolver
	shutdown chan struct{}
	logger   *logrus.Logger
}

// NewResolverService creates a new resolver service
func NewResolverService(r config.DNSResolver, logger *logrus.Logger) *ResolverService {
	return &ResolverService{resolver: r,
		shutdown: make(chan struct{}),
		logger:   logger,
	}
}

// Name is used to figure out this service is related to the others (normally the addr it binds to)
// this is just "resolver" since there can only be one DNS resolver running
func (s *ResolverService) Name() string {
	return ResolverServiceType
}

// Type is used to identify what kind of overwatch service this is
func (s *ResolverService) Type() string {
	return ResolverServiceType
}

// Hash is used to figure out if this forwarder is the unchanged or not from the config file updates
func (s *ResolverService) Hash() string {
	return s.resolver.Hash()
}

// Shutdown stops the tunneldns listener
func (s *ResolverService) Shutdown() {
	s.shutdown <- struct{}{}
}

// Run is the run loop that is started by the overwatch service
func (s *ResolverService) Run() error {
	// create a listener
	l, err := tunneldns.CreateListener(s.resolver.AddressOrDefault(), s.resolver.PortOrDefault(),
		s.resolver.UpstreamsOrDefault(), s.resolver.BootstrapsOrDefault())
	if err != nil {
		return err
	}

	// start the listener.
	readySignal := make(chan struct{})
	err = l.Start(readySignal)
	if err != nil {
		l.Stop()
		return err
	}
	<-readySignal
	s.logger.Infof("start resolver on: %s:%d", s.resolver.AddressOrDefault(), s.resolver.PortOrDefault())

	// wait for shutdown signal
	<-s.shutdown
	s.logger.Infof("shutdown on: %s:%d", s.resolver.AddressOrDefault(), s.resolver.PortOrDefault())
	return l.Stop()
}
