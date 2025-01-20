package edgediscovery

import (
	"net"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/edgediscovery/allregions"
)

var (
	testLogger = zerolog.Nop()
	v4Addrs    = []*allregions.EdgeAddr{&addr0, &addr1, &addr2, &addr3}
	v6Addrs    = []*allregions.EdgeAddr{&addr4, &addr5, &addr6, &addr7}
	addr0      = allregions.EdgeAddr{
		TCP: &net.TCPAddr{
			IP:   net.ParseIP("123.4.5.0"),
			Port: 8000,
			Zone: "",
		},
		UDP: &net.UDPAddr{
			IP:   net.ParseIP("123.4.5.0"),
			Port: 8000,
			Zone: "",
		},
		IPVersion: allregions.V4,
	}
	addr1 = allregions.EdgeAddr{
		TCP: &net.TCPAddr{
			IP:   net.ParseIP("123.4.5.1"),
			Port: 8000,
			Zone: "",
		},
		UDP: &net.UDPAddr{
			IP:   net.ParseIP("123.4.5.1"),
			Port: 8000,
			Zone: "",
		},
		IPVersion: allregions.V4,
	}
	addr2 = allregions.EdgeAddr{
		TCP: &net.TCPAddr{
			IP:   net.ParseIP("123.4.5.2"),
			Port: 8000,
			Zone: "",
		},
		UDP: &net.UDPAddr{
			IP:   net.ParseIP("123.4.5.2"),
			Port: 8000,
			Zone: "",
		},
		IPVersion: allregions.V4,
	}
	addr3 = allregions.EdgeAddr{
		TCP: &net.TCPAddr{
			IP:   net.ParseIP("123.4.5.3"),
			Port: 8000,
			Zone: "",
		},
		UDP: &net.UDPAddr{
			IP:   net.ParseIP("123.4.5.3"),
			Port: 8000,
			Zone: "",
		},
		IPVersion: allregions.V4,
	}
	addr4 = allregions.EdgeAddr{
		TCP: &net.TCPAddr{
			IP:   net.ParseIP("2606:4700:a0::1"),
			Port: 8000,
			Zone: "",
		},
		UDP: &net.UDPAddr{
			IP:   net.ParseIP("2606:4700:a0::1"),
			Port: 8000,
			Zone: "",
		},
		IPVersion: allregions.V6,
	}
	addr5 = allregions.EdgeAddr{
		TCP: &net.TCPAddr{
			IP:   net.ParseIP("2606:4700:a0::2"),
			Port: 8000,
			Zone: "",
		},
		UDP: &net.UDPAddr{
			IP:   net.ParseIP("2606:4700:a0::2"),
			Port: 8000,
			Zone: "",
		},
		IPVersion: allregions.V6,
	}
	addr6 = allregions.EdgeAddr{
		TCP: &net.TCPAddr{
			IP:   net.ParseIP("2606:4700:a0::3"),
			Port: 8000,
			Zone: "",
		},
		UDP: &net.UDPAddr{
			IP:   net.ParseIP("2606:4700:a0::3"),
			Port: 8000,
			Zone: "",
		},
		IPVersion: allregions.V6,
	}
	addr7 = allregions.EdgeAddr{
		TCP: &net.TCPAddr{
			IP:   net.ParseIP("2606:4700:a0::4"),
			Port: 8000,
			Zone: "",
		},
		UDP: &net.UDPAddr{
			IP:   net.ParseIP("2606:4700:a0::4"),
			Port: 8000,
			Zone: "",
		},
		IPVersion: allregions.V6,
	}
)

func TestGiveBack(t *testing.T) {
	edge := MockEdge(&testLogger, []*allregions.EdgeAddr{&addr0, &addr1, &addr2, &addr3})

	// Give this connection an address
	assert.Equal(t, 4, edge.AvailableAddrs())
	const connID = 0
	addr, err := edge.GetAddr(connID)
	assert.NoError(t, err)
	assert.NotNil(t, addr)
	assert.Equal(t, 3, edge.AvailableAddrs())

	// Get it back
	edge.GiveBack(addr, false)
	assert.Equal(t, 4, edge.AvailableAddrs())
}

func TestRPCAndProxyShareSingleEdgeIP(t *testing.T) {
	// Make an edge with a single IP
	edge := MockEdge(&testLogger, []*allregions.EdgeAddr{&addr0})
	tunnelConnID := 0

	// Use the IP for a tunnel
	addrTunnel, err := edge.GetAddr(tunnelConnID)
	assert.NoError(t, err)

	// Ensure the IP can be used for RPC too
	addrRPC, err := edge.GetAddrForRPC()
	assert.NoError(t, err)
	assert.Equal(t, addrTunnel, addrRPC)
}

func TestGetAddrForRPC(t *testing.T) {
	edge := MockEdge(&testLogger, []*allregions.EdgeAddr{&addr0, &addr1, &addr2, &addr3})

	// Get a connection
	assert.Equal(t, 4, edge.AvailableAddrs())
	addr, err := edge.GetAddrForRPC()
	assert.NoError(t, err)
	assert.NotNil(t, addr)

	// Using an address for RPC shouldn't consume it
	assert.Equal(t, 4, edge.AvailableAddrs())

	// Get it back
	edge.GiveBack(addr, false)
	assert.Equal(t, 4, edge.AvailableAddrs())
}

func TestOnePerRegion(t *testing.T) {
	// Make an edge with only one address
	edge := MockEdge(&testLogger, []*allregions.EdgeAddr{&addr0, &addr1})

	// Use the only address
	const connID = 0
	a1, err := edge.GetAddr(connID)
	assert.NoError(t, err)
	assert.NotNil(t, a1)

	// if the first address is bad, get the second one
	a2, err := edge.GetDifferentAddr(connID, false)
	assert.NoError(t, err)
	assert.NotNil(t, a2)
	assert.NotEqual(t, a1, a2)

	// now that second one is bad, get the first one again
	a3, err := edge.GetDifferentAddr(connID, false)
	assert.NoError(t, err)
	assert.Equal(t, a1, a3)
}

func TestOnlyOneAddrLeft(t *testing.T) {
	// Make an edge with only one address
	edge := MockEdge(&testLogger, []*allregions.EdgeAddr{&addr0})

	// Use the only address
	const connID = 0
	addr, err := edge.GetAddr(connID)
	assert.NoError(t, err)
	assert.NotNil(t, addr)

	// If that edge address is "bad", there's no alternative address.
	_, err = edge.GetDifferentAddr(connID, false)
	assert.Error(t, err)

	// previously bad address should become available again on next iteration.
	addr, err = edge.GetDifferentAddr(connID, false)
	assert.NoError(t, err)
	assert.NotNil(t, addr)
}

func TestNoAddrsLeft(t *testing.T) {
	// Make an edge with no addresses
	edge := MockEdge(&testLogger, []*allregions.EdgeAddr{})

	_, err := edge.GetAddr(2)
	assert.Error(t, err)
	_, err = edge.GetAddrForRPC()
	assert.Error(t, err)
}

func TestGetAddr(t *testing.T) {
	edge := MockEdge(&testLogger, []*allregions.EdgeAddr{&addr0, &addr1, &addr2, &addr3})

	// Give this connection an address
	const connID = 0
	addr, err := edge.GetAddr(connID)
	assert.NoError(t, err)
	assert.NotNil(t, addr)

	// If the same connection requests another address, it should get the same one.
	addr2, err := edge.GetAddr(connID)
	assert.NoError(t, err)
	assert.Equal(t, addr, addr2)
}

func TestGetDifferentAddr(t *testing.T) {
	edge := MockEdge(&testLogger, []*allregions.EdgeAddr{&addr0, &addr1, &addr2, &addr3})

	// Give this connection an address
	assert.Equal(t, 4, edge.AvailableAddrs())
	const connID = 0
	addr, err := edge.GetAddr(connID)
	assert.NoError(t, err)
	assert.NotNil(t, addr)
	assert.Equal(t, 3, edge.AvailableAddrs())

	// If the same connection requests another address, it should get the same one.
	addr2, err := edge.GetDifferentAddr(connID, false)
	assert.NoError(t, err)
	assert.NotEqual(t, addr, addr2)
	assert.Equal(t, 3, edge.AvailableAddrs())
}

// MockEdge creates a Cloudflare Edge from arbitrary TCP addresses. Used for testing.
func MockEdge(log *zerolog.Logger, addrs []*allregions.EdgeAddr) *Edge {
	regions := allregions.NewNoResolve(addrs)
	return &Edge{
		log:     log,
		regions: regions,
	}
}
