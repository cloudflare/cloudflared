package supervisor

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/streamhandler"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/sirupsen/logrus"
)

type Supervisor struct {
	connManager         *connection.EdgeManager
	streamHandler       *streamhandler.StreamHandler
	autoupdater         *updater.AutoUpdater
	supportAutoupdate   bool
	newConfigChan       <-chan *pogs.ClientConfig
	useConfigResultChan chan<- *pogs.UseConfigurationResult
	state               *state
	logger              *logrus.Entry
}

func NewSupervisor(
	defaultClientConfig *pogs.ClientConfig,
	userCredential []byte,
	tlsConfig *tls.Config,
	serviceDiscoverer connection.EdgeServiceDiscoverer,
	cloudflaredConfig *connection.CloudflaredConfig,
	autoupdater *updater.AutoUpdater,
	supportAutoupdate bool,
	logger *logrus.Logger,
) (*Supervisor, error) {
	newConfigChan := make(chan *pogs.ClientConfig)
	useConfigResultChan := make(chan *pogs.UseConfigurationResult)
	streamHandler := streamhandler.NewStreamHandler(newConfigChan, useConfigResultChan, logger)
	invalidConfigs := streamHandler.UpdateConfig(defaultClientConfig.ReverseProxyConfigs)

	if len(invalidConfigs) > 0 {
		for _, invalidConfig := range invalidConfigs {
			logger.Errorf("Tunnel %+v is invalid, reason: %s", invalidConfig.Config, invalidConfig.Reason)
		}
		return nil, fmt.Errorf("At least 1 Tunnel config is invalid")
	}

	tunnelHostnames := make([]h2mux.TunnelHostname, len(defaultClientConfig.ReverseProxyConfigs))
	for i, reverseProxyConfig := range defaultClientConfig.ReverseProxyConfigs {
		tunnelHostnames[i] = reverseProxyConfig.TunnelHostname
	}
	defaultEdgeMgrConfigurable := &connection.EdgeManagerConfigurable{
		tunnelHostnames,
		defaultClientConfig.EdgeConnectionConfig,
	}
	return &Supervisor{
		connManager: connection.NewEdgeManager(streamHandler, defaultEdgeMgrConfigurable, userCredential, tlsConfig,
			serviceDiscoverer, cloudflaredConfig, logger),
		streamHandler:       streamHandler,
		autoupdater:         autoupdater,
		supportAutoupdate:   supportAutoupdate,
		newConfigChan:       newConfigChan,
		useConfigResultChan: useConfigResultChan,
		state:               newState(defaultClientConfig),
		logger:              logger.WithField("subsystem", "supervisor"),
	}, nil
}

func (s *Supervisor) Run(ctx context.Context) error {
	errGroup, groupCtx := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		return s.connManager.Run(groupCtx)
	})

	errGroup.Go(func() error {
		return s.listenToNewConfig(groupCtx)
	})

	errGroup.Go(func() error {
		return s.listenToShutdownSignal(groupCtx)
	})

	if s.supportAutoupdate {
		errGroup.Go(func() error {
			return s.autoupdater.Run(groupCtx)
		})
	}

	err := errGroup.Wait()
	s.logger.Warnf("Supervisor terminated, reason: %v", err)
	return err
}

func (s *Supervisor) listenToShutdownSignal(serveCtx context.Context) error {
	signals := make(chan os.Signal, 10)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)

	select {
	case <-serveCtx.Done():
		return serveCtx.Err()
	case sig := <-signals:
		return fmt.Errorf("received %v signal", sig)
	}
}

func (s *Supervisor) listenToNewConfig(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case newConfig := <-s.newConfigChan:
			s.useConfigResultChan <- s.notifySubsystemsNewConfig(newConfig)
		}
	}
}

func (s *Supervisor) notifySubsystemsNewConfig(newConfig *pogs.ClientConfig) *pogs.UseConfigurationResult {
	s.logger.Infof("Received configuration %v", newConfig.Version)
	if s.state.hasAppliedVersion(newConfig.Version) {
		s.logger.Infof("%v has been applied", newConfig.Version)
		return &pogs.UseConfigurationResult{
			Success: true,
		}
	}

	s.state.updateConfig(newConfig)
	var tunnelHostnames []h2mux.TunnelHostname
	for _, tunnelConfig := range newConfig.ReverseProxyConfigs {
		tunnelHostnames = append(tunnelHostnames, tunnelConfig.TunnelHostname)
	}
	// Update connManager configurable
	s.connManager.UpdateConfigurable(&connection.EdgeManagerConfigurable{
		tunnelHostnames,
		newConfig.EdgeConnectionConfig,
	})
	// Update streamHandler tunnelHostnameMapper mapping
	failedConfigs := s.streamHandler.UpdateConfig(newConfig.ReverseProxyConfigs)

	if s.supportAutoupdate {
		s.autoupdater.Update(newConfig.SupervisorConfig.AutoUpdateFrequency)
	}

	return &pogs.UseConfigurationResult{
		Success:       len(failedConfigs) == 0,
		FailedConfigs: failedConfigs,
	}
}

type state struct {
	sync.RWMutex
	currentConfig *pogs.ClientConfig
}

func newState(currentConfig *pogs.ClientConfig) *state {
	return &state{
		currentConfig: currentConfig,
	}
}

func (s *state) hasAppliedVersion(incomingVersion pogs.Version) bool {
	s.RLock()
	defer s.RUnlock()
	return s.currentConfig.Version.IsNewerOrEqual(incomingVersion)
}

func (s *state) updateConfig(newConfig *pogs.ClientConfig) {
	s.Lock()
	defer s.Unlock()
	s.currentConfig = newConfig
}
