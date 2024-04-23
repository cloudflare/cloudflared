package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/proxy"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// Orchestrator manages configurations, so they can be updatable during runtime
// properties are static, so it can be read without lock
// currentVersion and config are read/write infrequently, so their access are synchronized with RWMutex
// access to proxy is synchronized with atomic.Value, because it uses copy-on-write to provide scalable frequently
// read when update is infrequent
type Orchestrator struct {
	currentVersion int32
	// Used by UpdateConfig to make sure one update at a time
	lock sync.RWMutex
	// Underlying value is proxy.Proxy, can be read without the lock, but still needs the lock to update
	proxy atomic.Value
	// Set of internal ingress rules defined at cloudflared startup (separate from user-defined ingress rules)
	internalRules []ingress.Rule
	// cloudflared Configuration
	config *Config
	tags   []tunnelpogs.Tag
	log    *zerolog.Logger

	// orchestrator must not handle any more updates after shutdownC is closed
	shutdownC <-chan struct{}
	// Closing proxyShutdownC will close the previous proxy
	proxyShutdownC chan<- struct{}
}

func NewOrchestrator(ctx context.Context,
	config *Config,
	tags []tunnelpogs.Tag,
	internalRules []ingress.Rule,
	log *zerolog.Logger) (*Orchestrator, error) {
	o := &Orchestrator{
		// Lowest possible version, any remote configuration will have version higher than this
		// Starting at -1 allows a configuration migration (local to remote) to override the current configuration as it
		// will start at version 0.
		currentVersion: -1,
		internalRules:  internalRules,
		config:         config,
		tags:           tags,
		log:            log,
		shutdownC:      ctx.Done(),
	}
	if err := o.updateIngress(*config.Ingress, config.WarpRouting); err != nil {
		return nil, err
	}
	go o.waitToCloseLastProxy()
	return o, nil
}

// UpdateConfig creates a new proxy with the new ingress rules
func (o *Orchestrator) UpdateConfig(version int32, config []byte) *tunnelpogs.UpdateConfigurationResponse {
	o.lock.Lock()
	defer o.lock.Unlock()

	if o.currentVersion >= version {
		o.log.Debug().
			Int32("current_version", o.currentVersion).
			Int32("received_version", version).
			Msg("Current version is equal or newer than received version")
		return &tunnelpogs.UpdateConfigurationResponse{
			LastAppliedVersion: o.currentVersion,
		}
	}
	var newConf newRemoteConfig
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

	if err := o.updateIngress(newConf.Ingress, newConf.WarpRouting); err != nil {
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
func (o *Orchestrator) updateIngress(ingressRules ingress.Ingress, warpRouting ingress.WarpRoutingConfig) error {
	select {
	case <-o.shutdownC:
		return fmt.Errorf("cloudflared already shutdown")
	default:
	}

	// Assign the internal ingress rules to the parsed ingress
	ingressRules.InternalRules = o.internalRules

	// Check if ingress rules are empty, and add the default route if so.
	if ingressRules.IsEmpty() {
		ingressRules.Rules = ingress.GetDefaultIngressRules(o.log)
	}

	// Start new proxy before closing the ones from last version.
	// The upside is we don't need to restart proxy from last version, which can fail
	// The downside is new version might have ingress rule that require previous version to be shutdown first
	// The downside is minimized because none of the ingress.OriginService implementation have that requirement
	proxyShutdownC := make(chan struct{})
	if err := ingressRules.StartOrigins(o.log, proxyShutdownC); err != nil {
		return errors.Wrap(err, "failed to start origin")
	}
	proxy := proxy.NewOriginProxy(ingressRules, warpRouting, o.tags, o.config.WriteTimeout, o.log)
	o.proxy.Store(proxy)
	o.config.Ingress = &ingressRules
	o.config.WarpRouting = warpRouting

	// If proxyShutdownC is nil, there is no previous running proxy
	if o.proxyShutdownC != nil {
		close(o.proxyShutdownC)
	}
	o.proxyShutdownC = proxyShutdownC
	return nil
}

// GetConfigJSON returns the current json serialization of the config as the edge understands it
func (o *Orchestrator) GetConfigJSON() ([]byte, error) {
	o.lock.RLock()
	defer o.lock.RUnlock()

	c := &newLocalConfig{
		RemoteConfig: ingress.RemoteConfig{
			Ingress:     *o.config.Ingress,
			WarpRouting: o.config.WarpRouting,
		},
		ConfigurationFlags: o.config.ConfigurationFlags,
	}

	return json.Marshal(c)
}

// GetVersionedConfigJSON returns the current version and configuration as JSON
func (o *Orchestrator) GetVersionedConfigJSON() ([]byte, error) {
	o.lock.RLock()
	defer o.lock.RUnlock()
	var currentConfiguration = struct {
		Version int32 `json:"version"`
		Config  struct {
			Ingress       []ingress.Rule              `json:"ingress"`
			WarpRouting   config.WarpRoutingConfig    `json:"warp-routing"`
			OriginRequest ingress.OriginRequestConfig `json:"originRequest"`
		} `json:"config"`
	}{
		Version: o.currentVersion,
		Config: struct {
			Ingress       []ingress.Rule              `json:"ingress"`
			WarpRouting   config.WarpRoutingConfig    `json:"warp-routing"`
			OriginRequest ingress.OriginRequestConfig `json:"originRequest"`
		}{
			Ingress:       o.config.Ingress.Rules,
			WarpRouting:   o.config.WarpRouting.RawConfig(),
			OriginRequest: o.config.Ingress.Defaults,
		},
	}
	return json.Marshal(currentConfiguration)
}

// GetOriginProxy returns an interface to proxy to origin. It satisfies connection.ConfigManager interface
func (o *Orchestrator) GetOriginProxy() (connection.OriginProxy, error) {
	val := o.proxy.Load()
	if val == nil {
		err := fmt.Errorf("origin proxy not configured")
		o.log.Error().Msg(err.Error())
		return nil, err
	}
	proxy, ok := val.(connection.OriginProxy)
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
