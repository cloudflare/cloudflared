package main

import (
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/overwatch"
)

// AppService is the main service that runs when no command lines flags are passed to cloudflared
// it manages all the running services such as tunnels, forwarders, DNS resolver, etc
type AppService struct {
	configManager    config.Manager
	serviceManager   overwatch.Manager
	shutdownC        chan struct{}
	configUpdateChan chan config.Root
	log              *zerolog.Logger
}

// NewAppService creates a new AppService with needed supporting services
func NewAppService(configManager config.Manager, serviceManager overwatch.Manager, shutdownC chan struct{}, log *zerolog.Logger) *AppService {
	return &AppService{
		configManager:    configManager,
		serviceManager:   serviceManager,
		shutdownC:        shutdownC,
		configUpdateChan: make(chan config.Root),
		log:              log,
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
	s.shutdownC <- struct{}{}

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
			for _, service := range s.serviceManager.Services() {
				service.Shutdown()
			}
			return
		}
	}
}

func (s *AppService) handleConfigUpdate(c config.Root) {
	// handle the client forward listeners
	activeServices := map[string]struct{}{}
	for _, f := range c.Forwarders {
		service := NewForwardService(f, s.log)
		s.serviceManager.Add(service)
		activeServices[service.Name()] = struct{}{}
	}

	// handle resolver changes
	if c.Resolver.Enabled {
		service := NewResolverService(c.Resolver, s.log)
		s.serviceManager.Add(service)
		activeServices[service.Name()] = struct{}{}

	}

	// TODO: TUN-1451 - tunnels

	// remove any services that are no longer active
	for _, service := range s.serviceManager.Services() {
		if _, ok := activeServices[service.Name()]; !ok {
			s.serviceManager.Remove(service.Name())
		}
	}
}
