package tunnelhostnamemapper

import (
	"sync"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/originservice"
)

// TunnelHostnameMapper maps TunnelHostname to an OriginService
type TunnelHostnameMapper struct {
	sync.RWMutex
	tunnelHostnameToOrigin map[h2mux.TunnelHostname]originservice.OriginService
}

func NewTunnelHostnameMapper() *TunnelHostnameMapper {
	return &TunnelHostnameMapper{
		tunnelHostnameToOrigin: make(map[h2mux.TunnelHostname]originservice.OriginService),
	}
}

// Get an OriginService given a TunnelHostname
func (om *TunnelHostnameMapper) Get(key h2mux.TunnelHostname) (originservice.OriginService, bool) {
	om.RLock()
	defer om.RUnlock()
	originService, ok := om.tunnelHostnameToOrigin[key]
	return originService, ok
}

// Add a mapping. If there is already an OriginService with this key, shutdown the old origin service and replace it
// with the new one
func (om *TunnelHostnameMapper) Add(key h2mux.TunnelHostname, os originservice.OriginService) {
	om.Lock()
	defer om.Unlock()
	if oldOS, ok := om.tunnelHostnameToOrigin[key]; ok {
		oldOS.Shutdown()
	}
	om.tunnelHostnameToOrigin[key] = os
}

// DeleteAll mappings, and shutdown all OriginService
func (om *TunnelHostnameMapper) DeleteAll() {
	om.Lock()
	defer om.Unlock()
	for key, os := range om.tunnelHostnameToOrigin {
		os.Shutdown()
		delete(om.tunnelHostnameToOrigin, key)
	}
}
