package edgediscovery

import (
	"sync"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
	"github.com/cloudflare/cloudflared/management"
)

const (
	LogFieldConnIndex = "connIndex"
	LogFieldIPAddress = "ip"
)

var errNoAddressesLeft = ErrNoAddressesLeft{}

type ErrNoAddressesLeft struct{}

func (e ErrNoAddressesLeft) Error() string {
	return "there are no free edge addresses left to resolve to"
}

// Edge finds addresses on the Cloudflare edge and hands them out to connections.
type Edge struct {
	regions *allregions.Regions
	sync.Mutex
	log *zerolog.Logger
}

// ------------------------------------
// Constructors
// ------------------------------------

// ResolveEdge runs the initial discovery of the Cloudflare edge, finding Addrs that can be allocated
// to connections.
func ResolveEdge(log *zerolog.Logger, region string, edgeIpVersion allregions.ConfigIPVersion) (*Edge, error) {
	regions, err := allregions.ResolveEdge(log, region, edgeIpVersion)
	if err != nil {
		return new(Edge), err
	}
	return &Edge{
		log:     log,
		regions: regions,
	}, nil
}

// StaticEdge creates a list of edge addresses from the list of hostnames. Mainly used for testing connectivity.
func StaticEdge(log *zerolog.Logger, hostnames []string) (*Edge, error) {
	regions, err := allregions.StaticEdge(hostnames, log)
	if err != nil {
		return new(Edge), err
	}
	return &Edge{
		log:     log,
		regions: regions,
	}, nil
}

// ------------------------------------
// Methods
// ------------------------------------

// GetAddrForRPC gives this connection an edge Addr.
func (ed *Edge) GetAddrForRPC() (*allregions.EdgeAddr, error) {
	ed.Lock()
	defer ed.Unlock()
	addr := ed.regions.GetAnyAddress()
	if addr == nil {
		return nil, errNoAddressesLeft
	}
	return addr, nil
}

// GetAddr gives this proxy connection an edge Addr. Prefer Addrs this connection has already used.
func (ed *Edge) GetAddr(connIndex int) (*allregions.EdgeAddr, error) {
	log := ed.log.With().
		Int(LogFieldConnIndex, connIndex).
		Int(management.EventTypeKey, int(management.Cloudflared)).
		Logger()
	ed.Lock()
	defer ed.Unlock()

	// If this connection has already used an edge addr, return it.
	if addr := ed.regions.AddrUsedBy(connIndex); addr != nil {
		log.Debug().IPAddr(LogFieldIPAddress, addr.UDP.IP).Msg("edge discovery: returning same edge address back to pool")
		return addr, nil
	}

	// Otherwise, give it an unused one
	addr := ed.regions.GetUnusedAddr(nil, connIndex)
	if addr == nil {
		log.Debug().Msg("edge discovery: no addresses left in pool to give proxy connection")
		return nil, errNoAddressesLeft
	}
	log.Debug().IPAddr(LogFieldIPAddress, addr.UDP.IP).Msg("edge discovery: giving new address to connection")
	return addr, nil
}

// GetDifferentAddr gives back the proxy connection's edge Addr and uses a new one.
func (ed *Edge) GetDifferentAddr(connIndex int, hasConnectivityError bool) (*allregions.EdgeAddr, error) {
	log := ed.log.With().
		Int(LogFieldConnIndex, connIndex).
		Int(management.EventTypeKey, int(management.Cloudflared)).
		Logger()
	ed.Lock()
	defer ed.Unlock()

	oldAddr := ed.regions.AddrUsedBy(connIndex)
	if oldAddr != nil {
		ed.regions.GiveBack(oldAddr, hasConnectivityError)
	}
	addr := ed.regions.GetUnusedAddr(oldAddr, connIndex)
	if addr == nil {
		log.Debug().Msg("edge discovery: no addresses left in pool to give proxy connection")
		// note: if oldAddr were not nil, it will become available on the next iteration
		return nil, errNoAddressesLeft
	}
	log.Debug().
		IPAddr(LogFieldIPAddress, addr.UDP.IP).
		Int("available", ed.regions.AvailableAddrs()).
		Msg("edge discovery: giving new address to connection")
	return addr, nil
}

// AvailableAddrs returns how many unused addresses there are left.
func (ed *Edge) AvailableAddrs() int {
	ed.Lock()
	defer ed.Unlock()
	return ed.regions.AvailableAddrs()
}

// GiveBack the address so that other connections can use it.
// Returns true if the address is in this edge.
func (ed *Edge) GiveBack(addr *allregions.EdgeAddr, hasConnectivityError bool) bool {
	ed.Lock()
	defer ed.Unlock()
	ed.log.Debug().
		Int(management.EventTypeKey, int(management.Cloudflared)).
		IPAddr(LogFieldIPAddress, addr.UDP.IP).
		Msg("edge discovery: gave back address to the pool")
	return ed.regions.GiveBack(addr, hasConnectivityError)
}
