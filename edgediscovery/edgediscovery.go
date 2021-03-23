package edgediscovery

import (
	"fmt"
	"net"
	"sync"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
)

const (
	LogFieldAddress   = "address"
	LogFieldConnIndex = "connIndex"
)

var errNoAddressesLeft = fmt.Errorf("there are no free edge addresses left")

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
func ResolveEdge(log *zerolog.Logger) (*Edge, error) {
	regions, err := allregions.ResolveEdge(log)
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

// MockEdge creates a Cloudflare Edge from arbitrary TCP addresses. Used for testing.
func MockEdge(log *zerolog.Logger, addrs []*net.TCPAddr) *Edge {
	regions := allregions.NewNoResolve(addrs)
	return &Edge{
		log:     log,
		regions: regions,
	}
}

// ------------------------------------
// Methods
// ------------------------------------

// GetAddrForRPC gives this connection an edge Addr.
func (ed *Edge) GetAddrForRPC() (*net.TCPAddr, error) {
	ed.Lock()
	defer ed.Unlock()
	addr := ed.regions.GetAnyAddress()
	if addr == nil {
		return nil, errNoAddressesLeft
	}
	return addr, nil
}

// GetAddr gives this proxy connection an edge Addr. Prefer Addrs this connection has already used.
func (ed *Edge) GetAddr(connIndex int) (*net.TCPAddr, error) {
	log := ed.log.With().Int(LogFieldConnIndex, connIndex).Logger()

	ed.Lock()
	defer ed.Unlock()

	// If this connection has already used an edge addr, return it.
	if addr := ed.regions.AddrUsedBy(connIndex); addr != nil {
		log.Debug().Msg("edgediscovery - GetAddr: Returning same address back to proxy connection")
		return addr, nil
	}

	// Otherwise, give it an unused one
	addr := ed.regions.GetUnusedAddr(nil, connIndex)
	if addr == nil {
		log.Debug().Msg("edgediscovery - GetAddr: No addresses left to give proxy connection")
		return nil, errNoAddressesLeft
	}
	log.Debug().Str(LogFieldAddress, addr.String()).Msg("edgediscovery - GetAddr: Giving connection its new address")
	return addr, nil
}

// GetDifferentAddr gives back the proxy connection's edge Addr and uses a new one.
func (ed *Edge) GetDifferentAddr(connIndex int) (*net.TCPAddr, error) {
	log := ed.log.With().Int(LogFieldConnIndex, connIndex).Logger()

	ed.Lock()
	defer ed.Unlock()

	oldAddr := ed.regions.AddrUsedBy(connIndex)
	if oldAddr != nil {
		ed.regions.GiveBack(oldAddr)
	}
	addr := ed.regions.GetUnusedAddr(oldAddr, connIndex)
	if addr == nil {
		log.Debug().Msg("edgediscovery - GetDifferentAddr: No addresses left to give proxy connection")
		// note: if oldAddr were not nil, it will become available on the next iteration
		return nil, errNoAddressesLeft
	}
	log.Debug().Str(LogFieldAddress, addr.String()).Msg("edgediscovery - GetDifferentAddr: Giving connection its new address")
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
func (ed *Edge) GiveBack(addr *net.TCPAddr) bool {
	ed.Lock()
	defer ed.Unlock()
	ed.log.Debug().Msg("edgediscovery - GiveBack: Address now unused")
	return ed.regions.GiveBack(addr)
}
