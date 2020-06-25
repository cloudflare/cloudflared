package allregions

import (
	"fmt"
	"net"

	"github.com/cloudflare/cloudflared/logger"
)

// Regions stores Cloudflare edge network IPs, partitioned into two regions.
// This is NOT thread-safe. Users of this package should use it with a lock.
type Regions struct {
	region1 Region
	region2 Region
}

// ------------------------------------
// Constructors
// ------------------------------------

// ResolveEdge resolves the Cloudflare edge, returning all regions discovered.
func ResolveEdge(logger logger.Service) (*Regions, error) {
	addrLists, err := edgeDiscovery(logger)
	if err != nil {
		return nil, err
	}
	if len(addrLists) < 2 {
		return nil, fmt.Errorf("expected at least 2 Cloudflare Regions regions, but SRV only returned %v", len(addrLists))
	}
	return &Regions{
		region1: NewRegion(addrLists[0]),
		region2: NewRegion(addrLists[1]),
	}, nil
}

// StaticEdge creates a list of edge addresses from the list of hostnames.
// Mainly used for testing connectivity.
func StaticEdge(hostnames []string) (*Regions, error) {
	addrs, err := ResolveAddrs(hostnames)
	if err != nil {
		return nil, err
	}
	return NewNoResolve(addrs), nil
}

// NewNoResolve doesn't resolve the edge. Instead it just uses the given addresses.
// You probably only need this for testing.
func NewNoResolve(addrs []*net.TCPAddr) *Regions {
	region1 := make([]*net.TCPAddr, 0)
	region2 := make([]*net.TCPAddr, 0)
	for i, v := range addrs {
		if i%2 == 0 {
			region1 = append(region1, v)
		} else {
			region2 = append(region2, v)
		}
	}

	return &Regions{
		region1: NewRegion(region1),
		region2: NewRegion(region2),
	}
}

// ------------------------------------
// Methods
// ------------------------------------

// GetAnyAddress returns an arbitrary address from the larger region.
func (rs *Regions) GetAnyAddress() *net.TCPAddr {
	if addr := rs.region1.GetAnyAddress(); addr != nil {
		return addr
	}
	return rs.region2.GetAnyAddress()
}

// AddrUsedBy finds the address used by the given connection.
// Returns nil if the connection isn't using an address.
func (rs *Regions) AddrUsedBy(connID int) *net.TCPAddr {
	if addr := rs.region1.AddrUsedBy(connID); addr != nil {
		return addr
	}
	return rs.region2.AddrUsedBy(connID)
}

// GetUnusedAddr gets an unused addr from the edge, excluding the given addr. Prefer to use addresses
// evenly across both regions.
func (rs *Regions) GetUnusedAddr(excluding *net.TCPAddr, connID int) *net.TCPAddr {
	if rs.region1.AvailableAddrs() > rs.region2.AvailableAddrs() {
		return getAddrs(excluding, connID, &rs.region1, &rs.region2)
	}

	return getAddrs(excluding, connID, &rs.region2, &rs.region1)
}

// getAddrs tries to grab address form `first` region, then `second` region
// this is an unrolled loop over 2 element array
func getAddrs(excluding *net.TCPAddr, connID int, first *Region, second *Region) *net.TCPAddr {
	addr := first.GetUnusedIP(excluding)
	if addr != nil {
		first.Use(addr, connID)
		return addr
	}
	addr = second.GetUnusedIP(excluding)
	if addr != nil {
		second.Use(addr, connID)
		return addr
	}

	return nil
}

// AvailableAddrs returns how many edge addresses aren't used.
func (rs *Regions) AvailableAddrs() int {
	return rs.region1.AvailableAddrs() + rs.region2.AvailableAddrs()
}

// GiveBack the address so that other connections can use it.
// Returns true if the address is in this edge.
func (rs *Regions) GiveBack(addr *net.TCPAddr) bool {
	if found := rs.region1.GiveBack(addr); found {
		return found
	}
	return rs.region2.GiveBack(addr)
}
