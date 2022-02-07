package supervisor

import (
	"sync"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/proxy"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

type configManager struct {
	currentVersion int32
	// Only used by UpdateConfig
	updateLock sync.Mutex
	// TODO: TUN-5698: Make proxy atomic.Value
	proxy  *proxy.Proxy
	config *DynamicConfig
	tags   []tunnelpogs.Tag
	log    *zerolog.Logger
}

func newConfigManager(config *DynamicConfig, tags []tunnelpogs.Tag, log *zerolog.Logger) *configManager {
	var warpRoutingService *ingress.WarpRoutingService
	if config.WarpRoutingEnabled {
		warpRoutingService = ingress.NewWarpRoutingService()
		log.Info().Msgf("Warp-routing is enabled")
	}

	return &configManager{
		// Lowest possible version, any remote configuration will have version higher than this
		currentVersion: 0,
		proxy:          proxy.NewOriginProxy(config.Ingress, warpRoutingService, tags, log),
		config:         config,
		log:            log,
	}
}

func (cm *configManager) Update(version int32, config []byte) *tunnelpogs.UpdateConfigurationResponse {
	// TODO: TUN-5698: make ingress configurable
	return &tunnelpogs.UpdateConfigurationResponse{
		LastAppliedVersion: cm.currentVersion,
	}
}

func (cm *configManager) GetOriginProxy() connection.OriginProxy {
	return cm.proxy
}

type DynamicConfig struct {
	Ingress            *ingress.Ingress
	WarpRoutingEnabled bool
}
