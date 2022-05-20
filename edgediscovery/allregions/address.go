package allregions

// Region contains cloudflared edge addresses. The edge is partitioned into several regions for
// redundancy purposes.
type AddrSet map[*EdgeAddr]UsedBy

// AddrUsedBy finds the address used by the given connection in this region.
// Returns nil if the connection isn't using any IP.
func (a AddrSet) AddrUsedBy(connID int) *EdgeAddr {
	for addr, used := range a {
		if used.Used && used.ConnID == connID {
			return addr
		}
	}
	return nil
}

// AvailableAddrs counts how many unused addresses this region contains.
func (a AddrSet) AvailableAddrs() int {
	n := 0
	for _, usedby := range a {
		if !usedby.Used {
			n++
		}
	}
	return n
}

// GetUnusedIP returns a random unused address in this region.
// Returns nil if all addresses are in use.
func (a AddrSet) GetUnusedIP(excluding *EdgeAddr) *EdgeAddr {
	for addr, usedby := range a {
		if !usedby.Used && addr != excluding {
			return addr
		}
	}
	return nil
}

// Use the address, assigning it to a proxy connection.
func (a AddrSet) Use(addr *EdgeAddr, connID int) {
	if addr == nil {
		return
	}
	a[addr] = InUse(connID)
}

// GetAnyAddress returns an arbitrary address from the region.
func (a AddrSet) GetAnyAddress() *EdgeAddr {
	for addr := range a {
		return addr
	}
	return nil
}

// GiveBack the address, ensuring it is no longer assigned to an IP.
// Returns true if the address is in this region.
func (a AddrSet) GiveBack(addr *EdgeAddr) (ok bool) {
	if _, ok := a[addr]; !ok {
		return false
	}
	a[addr] = Unused()
	return true
}
