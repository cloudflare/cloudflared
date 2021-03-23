package main

import (
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/tunneldns"
)

const (
	// ResolverServiceType is used to identify what kind of overwatch service this is
	ResolverServiceType = "resolver"

	LogFieldResolverAddress          = "resolverAddress"
	LogFieldResolverPort             = "resolverPort"
	LogFieldResolverMaxUpstreamConns = "resolverMaxUpstreamConns"
)

// ResolverService is used to wrap the tunneldns package's DNS over HTTP
// into a service model for the overwatch package.
// it also holds a reference to the config object that represents its state
type ResolverService struct {
	resolver config.DNSResolver
	shutdown chan struct{}
	log      *zerolog.Logger
}

// NewResolverService creates a new resolver service
func NewResolverService(r config.DNSResolver, log *zerolog.Logger) *ResolverService {
	return &ResolverService{resolver: r,
		shutdown: make(chan struct{}),
		log:      log,
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
		s.resolver.UpstreamsOrDefault(), s.resolver.BootstrapsOrDefault(), s.resolver.MaxUpstreamConnectionsOrDefault(), s.log)
	if err != nil {
		return err
	}

	// start the listener.
	readySignal := make(chan struct{})
	err = l.Start(readySignal)
	if err != nil {
		_ = l.Stop()
		return err
	}
	<-readySignal

	resolverLog := s.log.With().
		Str(LogFieldResolverAddress, s.resolver.AddressOrDefault()).
		Uint16(LogFieldResolverPort, s.resolver.PortOrDefault()).
		Int(LogFieldResolverMaxUpstreamConns, s.resolver.MaxUpstreamConnectionsOrDefault()).
		Logger()

	resolverLog.Info().Msg("Starting resolver")

	// wait for shutdown signal
	<-s.shutdown
	resolverLog.Info().Msg("Shutting down resolver")
	return l.Stop()
}
