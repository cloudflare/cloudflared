package allregions

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	addr0 = EdgeAddr{
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
	}
	addr1 = EdgeAddr{
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
	}
	addr2 = EdgeAddr{
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
	}
	addr3 = EdgeAddr{
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
	}
)

func makeRegions() Regions {
	r1 := NewRegion([]*EdgeAddr{&addr0, &addr1})
	r2 := NewRegion([]*EdgeAddr{&addr2, &addr3})
	return Regions{region1: r1, region2: r2}
}

func TestRegions_AddrUsedBy(t *testing.T) {
	rs := makeRegions()
	addr1 := rs.GetUnusedAddr(nil, 1)
	assert.Equal(t, addr1, rs.AddrUsedBy(1))
	addr2 := rs.GetUnusedAddr(nil, 2)
	assert.Equal(t, addr2, rs.AddrUsedBy(2))
	addr3 := rs.GetUnusedAddr(nil, 3)
	assert.Equal(t, addr3, rs.AddrUsedBy(3))
}

func TestRegions_Giveback_Region1(t *testing.T) {
	rs := makeRegions()
	rs.region1.Use(&addr0, 0)
	rs.region1.Use(&addr1, 1)
	rs.region2.Use(&addr2, 2)
	rs.region2.Use(&addr3, 3)

	assert.Equal(t, 0, rs.AvailableAddrs())

	rs.GiveBack(&addr0)
	assert.Equal(t, &addr0, rs.GetUnusedAddr(nil, 3))
}

func TestRegions_Giveback_Region2(t *testing.T) {
	rs := makeRegions()
	rs.region1.Use(&addr0, 0)
	rs.region1.Use(&addr1, 1)
	rs.region2.Use(&addr2, 2)
	rs.region2.Use(&addr3, 3)

	assert.Equal(t, 0, rs.AvailableAddrs())

	rs.GiveBack(&addr2)
	assert.Equal(t, &addr2, rs.GetUnusedAddr(nil, 2))
}

func TestRegions_GetUnusedAddr_OneAddrLeft(t *testing.T) {
	rs := makeRegions()

	rs.region1.Use(&addr0, 0)
	rs.region1.Use(&addr1, 1)
	rs.region2.Use(&addr2, 2)

	assert.Equal(t, 1, rs.AvailableAddrs())
	assert.Equal(t, &addr3, rs.GetUnusedAddr(nil, 3))
}

func TestRegions_GetUnusedAddr_Excluding_Region1(t *testing.T) {
	rs := makeRegions()

	rs.region1.Use(&addr0, 0)
	rs.region1.Use(&addr1, 1)

	assert.Equal(t, 2, rs.AvailableAddrs())
	assert.Equal(t, &addr3, rs.GetUnusedAddr(&addr2, 3))
}

func TestRegions_GetUnusedAddr_Excluding_Region2(t *testing.T) {
	rs := makeRegions()

	rs.region2.Use(&addr2, 0)
	rs.region2.Use(&addr3, 1)

	assert.Equal(t, 2, rs.AvailableAddrs())
	assert.Equal(t, &addr1, rs.GetUnusedAddr(&addr0, 1))
}

func TestNewNoResolveBalancesRegions(t *testing.T) {
	type args struct {
		addrs []*EdgeAddr
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "one address",
			args: args{addrs: []*EdgeAddr{&addr0}},
		},
		{
			name: "two addresses",
			args: args{addrs: []*EdgeAddr{&addr0, &addr1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regions := NewNoResolve(tt.args.addrs)
			RegionsIsBalanced(t, regions)
		})
	}
}

func RegionsIsBalanced(t *testing.T, rs *Regions) {
	delta := rs.region1.AvailableAddrs() - rs.region2.AvailableAddrs()
	assert.True(t, abs(delta) <= 1)
}

func abs(x int) int {
	if x >= 0 {
		return x
	}
	return -x
}
