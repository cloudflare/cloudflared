package edgediscovery

import (
	"fmt"
	"net"
	"sync"

	"github.com/cloudflare/cloudflared/edgediscovery/allregions"

	"github.com/sirupsen/logrus"
)

const (
	subsystem = "edgediscovery"
)

var errNoAddressesLeft = fmt.Errorf("There are no free edge addresses left")

// Edge finds addresses on the Cloudflare edge and hands them out to connections.
type Edge struct {
	regions *allregions.Regions
	sync.Mutex
	logger *logrus.Entry
}

// ------------------------------------
// Constructors
// ------------------------------------

// ResolveEdge runs the initial discovery of the Cloudflare edge, finding Addrs that can be allocated
// to connections.
func ResolveEdge(l *logrus.Logger) (*Edge, error) {
	logger := l.WithField("subsystem", subsystem)
	regions, err := allregions.ResolveEdge(logger)
	if err != nil {
		return new(Edge), err
	}
	return &Edge{
		logger:  logger,
		regions: regions,
	}, nil
}

// StaticEdge creates a list of edge addresses from the list of hostnames. Mainly used for testing connectivity.
func StaticEdge(l *logrus.Logger, hostnames []string) (*Edge, error) {
	logger := l.WithField("subsystem", subsystem)
	regions, err := allregions.StaticEdge(hostnames)
	if err != nil {
		return new(Edge), err
	}
	return &Edge{
		logger:  logger,
		regions: regions,
	}, nil
}

// MockEdge creates a Cloudflare Edge from arbitrary TCP addresses. Used for testing.
func MockEdge(l *logrus.Logger, addrs []*net.TCPAddr) *Edge {
	logger := l.WithField("subsystem", subsystem)
	regions := allregions.NewNoResolve(addrs)
	return &Edge{
		logger:  logger,
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
func (ed *Edge) GetAddr(connID int) (*net.TCPAddr, error) {
	ed.Lock()
	defer ed.Unlock()
	logger := ed.logger.WithFields(logrus.Fields{
		"connID":   connID,
		"function": "GetAddr",
	})

	// If this connection has already used an edge addr, return it.
	if addr := ed.regions.AddrUsedBy(connID); addr != nil {
		logger.Debug("Returning same address back to proxy connection")
		return addr, nil
	}

	// Otherwise, give it an unused one
	addr := ed.regions.GetUnusedAddr(nil, connID)
	if addr == nil {
		logger.Debug("No addresses left to give proxy connection")
		return nil, errNoAddressesLeft
	}
	logger.Debug("Giving connection its new address")
	return addr, nil
}

// GetDifferentAddr gives back the proxy connection's edge Addr and uses a new one.
func (ed *Edge) GetDifferentAddr(connID int) (*net.TCPAddr, error) {
	ed.Lock()
	defer ed.Unlock()
	logger := ed.logger.WithFields(logrus.Fields{
		"connID":   connID,
		"function": "GetDifferentAddr",
	})

	oldAddr := ed.regions.AddrUsedBy(connID)
	if oldAddr != nil {
		ed.regions.GiveBack(oldAddr)
	}
	addr := ed.regions.GetUnusedAddr(oldAddr, connID)
	if addr == nil {
		logger.Debug("No addresses left to give proxy connection")
		return nil, errNoAddressesLeft
	}
	logger.Debug("Giving connection its new address")
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
	ed.logger.WithField("function", "GiveBack").Debug("Address now unused")
	return ed.regions.GiveBack(addr)
}
