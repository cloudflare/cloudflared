package tunnelhostnamemapper

import (
	"sync"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/originservice"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
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

// Delete a mapping, and shutdown its OriginService
func (om *TunnelHostnameMapper) Delete(key h2mux.TunnelHostname) (keyFound bool) {
	om.Lock()
	defer om.Unlock()
	if os, ok := om.tunnelHostnameToOrigin[key]; ok {
		os.Shutdown()
		delete(om.tunnelHostnameToOrigin, key)
		return true
	}
	return false
}

// ToRemove finds all keys that should be removed from the TunnelHostnameMapper.
func (om *TunnelHostnameMapper) ToRemove(newConfigs []*pogs.ReverseProxyConfig) (toRemove []h2mux.TunnelHostname) {
	om.Lock()
	defer om.Unlock()

	// Convert into a set, for O(1) lookups instead of O(n)
	newConfigSet := toSet(newConfigs)

	// If a config in `om` isn't in `newConfigs`, it must be removed.
	for hostname := range om.tunnelHostnameToOrigin {
		if _, ok := newConfigSet[hostname]; !ok {
			toRemove = append(toRemove, hostname)
		}
	}

	return
}

// ToAdd filters the given configs, keeping those that should be added to the TunnelHostnameMapper.
func (om *TunnelHostnameMapper) ToAdd(newConfigs []*pogs.ReverseProxyConfig) (toAdd []*pogs.ReverseProxyConfig) {
	om.Lock()
	defer om.Unlock()

	// If a config in `newConfigs` isn't in `om`, it must be added.
	for _, config := range newConfigs {
		if _, ok := om.tunnelHostnameToOrigin[config.TunnelHostname]; !ok {
			toAdd = append(toAdd, config)
		}
	}

	return
}

func toSet(configs []*pogs.ReverseProxyConfig) map[h2mux.TunnelHostname]*pogs.ReverseProxyConfig {
	m := make(map[h2mux.TunnelHostname]*pogs.ReverseProxyConfig)
	for _, config := range configs {
		m[config.TunnelHostname] = config
	}
	return m
}
