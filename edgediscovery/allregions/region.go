package allregions

import "time"

const (
	timeoutDuration = 10 * time.Minute
)

// Region contains cloudflared edge addresses. The edge is partitioned into several regions for
// redundancy purposes.
type Region struct {
	primaryIsActive bool
	active          AddrSet
	primary         AddrSet
	secondary       AddrSet
	primaryTimeout  time.Time
	timeoutDuration time.Duration
}

// NewRegion creates a region with the given addresses, which are all unused.
func NewRegion(addrs []*EdgeAddr, overrideIPVersion ConfigIPVersion) Region {
	// The zero value of UsedBy is Unused(), so we can just initialize the map's values with their
	// zero values.
	connForv4 := make(AddrSet)
	connForv6 := make(AddrSet)
	systemPreference := V6
	for i, addr := range addrs {
		if i == 0 {
			// First family of IPs returned is system preference of IP
			systemPreference = addr.IPVersion
		}
		switch addr.IPVersion {
		case V4:
			connForv4[addr] = Unused()
		case V6:
			connForv6[addr] = Unused()
		}
	}

	// Process as system preference
	var primary AddrSet
	var secondary AddrSet
	switch systemPreference {
	case V4:
		primary = connForv4
		secondary = connForv6
	case V6:
		primary = connForv6
		secondary = connForv4
	}

	// Override with provided preference
	switch overrideIPVersion {
	case IPv4Only:
		primary = connForv4
		secondary = make(AddrSet) // empty
	case IPv6Only:
		primary = connForv6
		secondary = make(AddrSet) // empty
	case Auto:
		// no change
	default:
		// no change
	}

	return Region{
		primaryIsActive: true,
		active:          primary,
		primary:         primary,
		secondary:       secondary,
		timeoutDuration: timeoutDuration,
	}
}

// AddrUsedBy finds the address used by the given connection in this region.
// Returns nil if the connection isn't using any IP.
func (r *Region) AddrUsedBy(connID int) *EdgeAddr {
	edgeAddr := r.primary.AddrUsedBy(connID)
	if edgeAddr == nil {
		edgeAddr = r.secondary.AddrUsedBy(connID)
	}
	return edgeAddr
}

// AvailableAddrs counts how many unused addresses this region contains.
func (r Region) AvailableAddrs() int {
	return r.active.AvailableAddrs()
}

// AssignAnyAddress returns a random unused address in this region now
// assigned to the connID excluding the provided EdgeAddr.
// Returns nil if all addresses are in use for the region.
func (r Region) AssignAnyAddress(connID int, excluding *EdgeAddr) *EdgeAddr {
	if addr := r.active.GetUnusedIP(excluding); addr != nil {
		r.active.Use(addr, connID)
		return addr
	}
	return nil
}

// GetAnyAddress returns an arbitrary address from the region.
func (r Region) GetAnyAddress() *EdgeAddr {
	return r.active.GetAnyAddress()
}

// GiveBack the address, ensuring it is no longer assigned to an IP.
// Returns true if the address is in this region.
func (r *Region) GiveBack(addr *EdgeAddr, hasConnectivityError bool) (ok bool) {
	if ok = r.primary.GiveBack(addr); !ok {
		// Attempt to give back the address in the secondary set
		if ok = r.secondary.GiveBack(addr); !ok {
			// Address is not in this region
			return
		}
	}

	// No connectivity error: no worry
	if !hasConnectivityError {
		return
	}

	// If using primary and returned address is IPv6 and secondary is available
	if r.primaryIsActive && addr.IPVersion == V6 && len(r.secondary) > 0 {
		r.active = r.secondary
		r.primaryIsActive = false
		r.primaryTimeout = time.Now().Add(r.timeoutDuration)
		return
	}

	// Do nothing for IPv4 or if secondary is empty
	if r.primaryIsActive {
		return
	}

	// Immediately return to primary pool, regardless of current primary timeout
	if addr.IPVersion == V4 {
		activatePrimary(r)
		return
	}

	// Timeout exceeded and can be reset to primary pool
	if r.primaryTimeout.Before(time.Now()) {
		activatePrimary(r)
		return
	}

	return
}

// activatePrimary sets the primary set to the active set and resets the timeout.
func activatePrimary(r *Region) {
	r.active = r.primary
	r.primaryIsActive = true
	r.primaryTimeout = time.Now() // reset timeout
}
