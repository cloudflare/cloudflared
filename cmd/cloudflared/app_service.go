package main

import (
	"github.com/cloudflare/cloudflared/cmd/cloudflared/access"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/sirupsen/logrus"
)

type forwarderState struct {
	forwarder config.Forwarder
	shutdown  chan struct{}
}

func (s *forwarderState) Shutdown() {
	s.shutdown <- struct{}{}
}

// AppService is the main service that runs when no command lines flags are passed to cloudflared
// it manages all the running services such as tunnels, forwarders, DNS resolver, etc
type AppService struct {
	configManager    config.Manager
	shutdownC        chan struct{}
	forwarders       map[string]forwarderState
	configUpdateChan chan config.Root
	logger           *logrus.Logger
}

// NewAppService creates a new AppService with needed supporting services
func NewAppService(configManager config.Manager, shutdownC chan struct{}, logger *logrus.Logger) *AppService {
	return &AppService{
		configManager:    configManager,
		shutdownC:        shutdownC,
		forwarders:       make(map[string]forwarderState),
		configUpdateChan: make(chan config.Root),
		logger:           logger,
	}
}

// Run starts the run loop to handle config updates and run forwarders, tunnels, etc
func (s *AppService) Run() error {
	go s.actionLoop()
	return s.configManager.Start(s)
}

// Shutdown kills all the running services
func (s *AppService) Shutdown() error {
	s.configManager.Shutdown()
	return nil
}

// ConfigDidUpdate is a delegate notification from the config manager
// it is trigger when the config file has been updated and now the service needs
// to update its services accordingly
func (s *AppService) ConfigDidUpdate(c config.Root) {
	s.configUpdateChan <- c
}

// actionLoop handles the actions from running processes
func (s *AppService) actionLoop() {
	for {
		select {
		case c := <-s.configUpdateChan:
			s.handleConfigUpdate(c)
		case <-s.shutdownC:
			for _, state := range s.forwarders {
				state.Shutdown()
			}
			return
		}
	}
}

func (s *AppService) handleConfigUpdate(c config.Root) {
	// handle the client forward listeners
	activeListeners := map[string]struct{}{}
	for _, f := range c.Forwarders {
		s.handleForwarderUpdate(f)
		activeListeners[f.Listener] = struct{}{}
	}

	// remove any listeners that are no longer active
	for key, state := range s.forwarders {
		if _, ok := activeListeners[key]; !ok {
			state.Shutdown()
			delete(s.forwarders, key)
		}
	}

	// TODO: AUTH-2588, TUN-1451 - tunnels and dns proxy
}

// handle managing a forwarder service
func (s *AppService) handleForwarderUpdate(f config.Forwarder) {
	// check if we need to start a new listener or stop an old one
	if state, ok := s.forwarders[f.Listener]; ok {
		if state.forwarder.Hash() == f.Hash() {
			return // the exact same listener, no changes, so move along
		}
		state.Shutdown() //shutdown the listener since a new one is starting
	}
	// add a new forwarder to the list
	state := forwarderState{forwarder: f, shutdown: make(chan struct{}, 1)}
	s.forwarders[f.Listener] = state

	// start the forwarder
	go func(f forwarderState) {
		err := access.StartForwarder(f.forwarder, f.shutdown)
		if err != nil {
			s.logger.WithError(err).Errorf("Forwarder at address: %s", f.forwarder)
		}
	}(state)
}
