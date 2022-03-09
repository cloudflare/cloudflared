package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/proxy"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// Orchestrator manages configurations so they can be updatable during runtime
// properties are static, so it can be read without lock
// currentVersion and config are read/write infrequently, so their access are synchronized with RWMutex
// access to proxy is synchronized with atmoic.Value, because it uses copy-on-write to provide scalable frequently
// read when update is infrequent
type Orchestrator struct {
	currentVersion int32
	// Used by UpdateConfig to make sure one update at a time
	lock sync.RWMutex
	// Underlying value is proxy.Proxy, can be read without the lock, but still needs the lock to update
	proxy  atomic.Value
	config *Config
	tags   []tunnelpogs.Tag
	log    *zerolog.Logger

	// orchestrator must not handle any more updates after shutdownC is closed
	shutdownC <-chan struct{}
	// Closing proxyShutdownC will close the previous proxy
	proxyShutdownC chan<- struct{}
}

func NewOrchestrator(ctx context.Context, config *Config, tags []tunnelpogs.Tag, log *zerolog.Logger) (*Orchestrator, error) {
	o := &Orchestrator{
		// Lowest possible version, any remote configuration will have version higher than this
		currentVersion: 0,
		config:         config,
		tags:           tags,
		log:            log,
		shutdownC:      ctx.Done(),
	}
	if err := o.updateIngress(*config.Ingress, config.WarpRoutingEnabled); err != nil {
		return nil, err
	}
	go o.waitToCloseLastProxy()
	return o, nil
}

// Update creates a new proxy with the new ingress rules
func (o *Orchestrator) UpdateConfig(version int32, config []byte) *tunnelpogs.UpdateConfigurationResponse {
	o.lock.Lock()
	defer o.lock.Unlock()

	if o.currentVersion >= version {
		o.log.Debug().
			Int32("current_version", o.currentVersion).
			Int32("received_version", version).
			Msg("Current version is equal or newer than receivied version")
		return &tunnelpogs.UpdateConfigurationResponse{
			LastAppliedVersion: o.currentVersion,
		}
	}
	var newConf newConfig
	if err := json.Unmarshal(config, &newConf); err != nil {
		o.log.Err(err).
			Int32("version", version).
			Str("config", string(config)).
			Msgf("Failed to deserialize new configuration")
		return &tunnelpogs.UpdateConfigurationResponse{
			LastAppliedVersion: o.currentVersion,
			Err:                err,
		}
	}

	if err := o.updateIngress(newConf.Ingress, newConf.WarpRouting.Enabled); err != nil {
		o.log.Err(err).
			Int32("version", version).
			Str("config", string(config)).
			Msgf("Failed to update ingress")
		return &tunnelpogs.UpdateConfigurationResponse{
			LastAppliedVersion: o.currentVersion,
			Err:                err,
		}
	}
	o.currentVersion = version

	o.log.Info().
		Int32("version", version).
		Str("config", string(config)).
		Msg("Updated to new configuration")
	configVersion.Set(float64(version))
	return &tunnelpogs.UpdateConfigurationResponse{
		LastAppliedVersion: o.currentVersion,
	}
}

// The caller is responsible to make sure there is no concurrent access
func (o *Orchestrator) updateIngress(ingressRules ingress.Ingress, warpRoutingEnabled bool) error {
	select {
	case <-o.shutdownC:
		return fmt.Errorf("cloudflared already shutdown")
	default:
	}

	// Start new proxy before closing the ones from last version.
	// The upside is we don't need to restart proxy from last version, which can fail
	// The downside is new version might have ingress rule that require previous version to be shutdown first
	// The downside is minimized because none of the ingress.OriginService implementation have that requirement
	proxyShutdownC := make(chan struct{})
	if err := ingressRules.StartOrigins(o.log, proxyShutdownC); err != nil {
		return errors.Wrap(err, "failed to start origin")
	}
	newProxy := proxy.NewOriginProxy(ingressRules, warpRoutingEnabled, o.tags, o.log)
	o.proxy.Store(newProxy)
	o.config.Ingress = &ingressRules
	o.config.WarpRoutingEnabled = warpRoutingEnabled

	// If proxyShutdownC is nil, there is no previous running proxy
	if o.proxyShutdownC != nil {
		close(o.proxyShutdownC)
	}
	o.proxyShutdownC = proxyShutdownC
	return nil
}

// GetOriginProxy returns an interface to proxy to origin. It satisfies connection.ConfigManager interface
func (o *Orchestrator) GetOriginProxy() (connection.OriginProxy, error) {
	val := o.proxy.Load()
	if val == nil {
		err := fmt.Errorf("origin proxy not configured")
		o.log.Error().Msg(err.Error())
		return nil, err
	}
	proxy, ok := val.(*proxy.Proxy)
	if !ok {
		err := fmt.Errorf("origin proxy has unexpected value %+v", val)
		o.log.Error().Msg(err.Error())
		return nil, err
	}
	return proxy, nil
}

func (o *Orchestrator) waitToCloseLastProxy() {
	<-o.shutdownC
	o.lock.Lock()
	defer o.lock.Unlock()

	if o.proxyShutdownC != nil {
		close(o.proxyShutdownC)
		o.proxyShutdownC = nil
	}
}
