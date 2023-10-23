package allregions

import (
	"fmt"
	"math/rand"

	"github.com/rs/zerolog"
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
func ResolveEdge(log *zerolog.Logger, region string, overrideIPVersion ConfigIPVersion) (*Regions, error) {
    edgeAddrs, err := edgeDiscovery(log, getRegionalServiceName(region))
    if err != nil {
      return nil, err
    }
    return &Regions{
      region1: NewRegion(edgeAddrs[0], overrideIPVersion),
    }, nil
}

// StaticEdge creates a list of edge addresses from the list of hostnames.
// Mainly used for testing connectivity.
func StaticEdge(hostnames []string, log *zerolog.Logger) (*Regions, error) {
	resolved := ResolveAddrs(hostnames, log)
	if len(resolved) == 0 {
		return nil, fmt.Errorf("failed to resolve any edge address")
	}
	return NewNoResolve(resolved), nil
}

// NewNoResolve doesn't resolve the edge. Instead it just uses the given addresses.
// You probably only need this for testing.
func NewNoResolve(addrs []*EdgeAddr) *Regions {
	region1 := make([]*EdgeAddr, 0)
	region2 := make([]*EdgeAddr, 0)
	for i, v := range addrs {
		if i%2 == 0 {
			region1 = append(region1, v)
		} else {
			region2 = append(region2, v)
		}
	}

	return &Regions{
		region1: NewRegion(region1, Auto),
		region2: NewRegion(region2, Auto),
	}
}

// ------------------------------------
// Methods
// ------------------------------------

// GetAnyAddress returns an arbitrary address from the larger region.
func (rs *Regions) GetAnyAddress() *EdgeAddr {
	if addr := rs.region1.GetAnyAddress(); addr != nil {
		return addr
	}
	return rs.region2.GetAnyAddress()
}

// AddrUsedBy finds the address used by the given connection.
// Returns nil if the connection isn't using an address.
func (rs *Regions) AddrUsedBy(connID int) *EdgeAddr {
	if addr := rs.region1.AddrUsedBy(connID); addr != nil {
		return addr
	}
	return rs.region2.AddrUsedBy(connID)
}

// GetUnusedAddr gets an unused addr from the edge, excluding the given addr. Prefer to use addresses
// evenly across both regions.
func (rs *Regions) GetUnusedAddr(excluding *EdgeAddr, connID int) *EdgeAddr {
	// If both regions have the same number of available addrs, lets randomise which one
	// we pick. The rest of this algorithm will continue to make sure we always use addresses
	// evenly across both regions.
	if rs.region1.AvailableAddrs() == rs.region2.AvailableAddrs() {
		regions := []Region{rs.region1, rs.region2}
		firstChoice := rand.Intn(2)
		return getAddrs(excluding, connID, &regions[firstChoice], &regions[1-firstChoice])
	}

	if rs.region1.AvailableAddrs() > rs.region2.AvailableAddrs() {
		return getAddrs(excluding, connID, &rs.region1, &rs.region2)
	}

	return getAddrs(excluding, connID, &rs.region2, &rs.region1)
}

// getAddrs tries to grab address form `first` region, then `second` region
// this is an unrolled loop over 2 element array
func getAddrs(excluding *EdgeAddr, connID int, first *Region, second *Region) *EdgeAddr {
	addr := first.AssignAnyAddress(connID, excluding)
	if addr != nil {
		return addr
	}
	addr = second.AssignAnyAddress(connID, excluding)
	if addr != nil {
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
func (rs *Regions) GiveBack(addr *EdgeAddr, hasConnectivityError bool) bool {
	if found := rs.region1.GiveBack(addr, hasConnectivityError); found {
		return found
	}
	return rs.region2.GiveBack(addr, hasConnectivityError)
}

// Return regionalized service name if `region` isn't empty, otherwise return the global service name for origintunneld
func getRegionalServiceName(region string) string {
	if region != "" {
		return region + "-" + srvService // Example: `us-v2-origintunneld`
	}

	return srvService // Global service is just `v2-origintunneld`
}
