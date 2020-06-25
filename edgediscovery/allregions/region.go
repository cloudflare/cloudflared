package allregions

import (
	"net"
)

// Region contains cloudflared edge addresses. The edge is partitioned into several regions for
// redundancy purposes.
type Region struct {
	connFor map[*net.TCPAddr]UsedBy
}

// NewRegion creates a region with the given addresses, which are all unused.
func NewRegion(addrs []*net.TCPAddr) Region {
	// The zero value of UsedBy is Unused(), so we can just initialize the map's values with their
	// zero values.
	m := make(map[*net.TCPAddr]UsedBy)
	for _, addr := range addrs {
		m[addr] = Unused()
	}
	return Region{connFor: m}
}

// AddrUsedBy finds the address used by the given connection in this region.
// Returns nil if the connection isn't using any IP.
func (r *Region) AddrUsedBy(connID int) *net.TCPAddr {
	for addr, used := range r.connFor {
		if used.Used && used.ConnID == connID {
			return addr
		}
	}
	return nil
}

// AvailableAddrs counts how many unused addresses this region contains.
func (r Region) AvailableAddrs() int {
	n := 0
	for _, usedby := range r.connFor {
		if !usedby.Used {
			n++
		}
	}
	return n
}

// GetUnusedIP returns a random unused address in this region.
// Returns nil if all addresses are in use.
func (r Region) GetUnusedIP(excluding *net.TCPAddr) *net.TCPAddr {
	for addr, usedby := range r.connFor {
		if !usedby.Used && addr != excluding {
			return addr
		}
	}
	return nil
}

// Use the address, assigning it to a proxy connection.
func (r Region) Use(addr *net.TCPAddr, connID int) {
	if addr == nil {
		//logrus.Errorf("Attempted to use nil address for connection %d", connID)
		return
	}
	r.connFor[addr] = InUse(connID)
}

// GetAnyAddress returns an arbitrary address from the region.
func (r Region) GetAnyAddress() *net.TCPAddr {
	for addr := range r.connFor {
		return addr
	}
	return nil
}

// GiveBack the address, ensuring it is no longer assigned to an IP.
// Returns true if the address is in this region.
func (r Region) GiveBack(addr *net.TCPAddr) (ok bool) {
	if _, ok := r.connFor[addr]; !ok {
		return false
	}
	r.connFor[addr] = Unused()
	return true
}
