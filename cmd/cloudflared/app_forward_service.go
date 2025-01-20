package main

import (
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/access"
	"github.com/cloudflare/cloudflared/config"
)

// ForwardServiceType is used to identify what kind of overwatch service this is
const ForwardServiceType = "forward"

// ForwarderService is used to wrap the access package websocket forwarders
// into a service model for the overwatch package.
// it also holds a reference to the config object that represents its state
type ForwarderService struct {
	forwarder config.Forwarder
	shutdown  chan struct{}
	log       *zerolog.Logger
}

// NewForwardService creates a new forwarder service
func NewForwardService(f config.Forwarder, log *zerolog.Logger) *ForwarderService {
	return &ForwarderService{forwarder: f, shutdown: make(chan struct{}, 1), log: log}
}

// Name is used to figure out this service is related to the others (normally the addr it binds to)
// e.g. localhost:78641 or 127.0.0.1:2222 since this is a websocket forwarder
func (s *ForwarderService) Name() string {
	return s.forwarder.Listener
}

// Type is used to identify what kind of overwatch service this is
func (s *ForwarderService) Type() string {
	return ForwardServiceType
}

// Hash is used to figure out if this forwarder is the unchanged or not from the config file updates
func (s *ForwarderService) Hash() string {
	return s.forwarder.Hash()
}

// Shutdown stops the websocket listener
func (s *ForwarderService) Shutdown() {
	s.shutdown <- struct{}{}
}

// Run is the run loop that is started by the overwatch service
func (s *ForwarderService) Run() error {
	return access.StartForwarder(s.forwarder, s.shutdown, s.log)
}
