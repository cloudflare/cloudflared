package allregions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func makeRegions(addrs []*EdgeAddr, mode ConfigIPVersion) Regions {
	r1addrs := make([]*EdgeAddr, 0)
	r2addrs := make([]*EdgeAddr, 0)
	for i, addr := range addrs {
		if i%2 == 0 {
			r1addrs = append(r1addrs, addr)
		} else {
			r2addrs = append(r2addrs, addr)
		}
	}
	r1 := NewRegion(r1addrs, mode)
	r2 := NewRegion(r2addrs, mode)
	return Regions{region1: r1, region2: r2}
}

func TestRegions_AddrUsedBy(t *testing.T) {
	tests := []struct {
		name  string
		addrs []*EdgeAddr
		mode  ConfigIPVersion
	}{
		{
			name:  "IPv4 addresses with IPv4Only",
			addrs: v4Addrs,
			mode:  IPv4Only,
		},
		{
			name:  "IPv6 addresses with IPv6Only",
			addrs: v6Addrs,
			mode:  IPv6Only,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := makeRegions(tt.addrs, tt.mode)
			addr1 := rs.GetUnusedAddr(nil, 1)
			assert.Equal(t, addr1, rs.AddrUsedBy(1))
			addr2 := rs.GetUnusedAddr(nil, 2)
			assert.Equal(t, addr2, rs.AddrUsedBy(2))
			addr3 := rs.GetUnusedAddr(nil, 3)
			assert.Equal(t, addr3, rs.AddrUsedBy(3))
		})
	}
}

func TestRegions_Giveback_Region1(t *testing.T) {
	tests := []struct {
		name  string
		addrs []*EdgeAddr
		mode  ConfigIPVersion
	}{
		{
			name:  "IPv4 addresses with IPv4Only",
			addrs: v4Addrs,
			mode:  IPv4Only,
		},
		{
			name:  "IPv6 addresses with IPv6Only",
			addrs: v6Addrs,
			mode:  IPv6Only,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := makeRegions(tt.addrs, tt.mode)
			addr := rs.region1.AssignAnyAddress(0, nil)
			rs.region1.AssignAnyAddress(1, nil)
			rs.region2.AssignAnyAddress(2, nil)
			rs.region2.AssignAnyAddress(3, nil)

			assert.Equal(t, 0, rs.AvailableAddrs())

			rs.GiveBack(addr, false)
			assert.Equal(t, addr, rs.GetUnusedAddr(nil, 0))
		})
	}
}

func TestRegions_Giveback_Region2(t *testing.T) {
	tests := []struct {
		name  string
		addrs []*EdgeAddr
		mode  ConfigIPVersion
	}{
		{
			name:  "IPv4 addresses with IPv4Only",
			addrs: v4Addrs,
			mode:  IPv4Only,
		},
		{
			name:  "IPv6 addresses with IPv6Only",
			addrs: v6Addrs,
			mode:  IPv6Only,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := makeRegions(tt.addrs, tt.mode)
			rs.region1.AssignAnyAddress(0, nil)
			rs.region1.AssignAnyAddress(1, nil)
			addr := rs.region2.AssignAnyAddress(2, nil)
			rs.region2.AssignAnyAddress(3, nil)

			assert.Equal(t, 0, rs.AvailableAddrs())

			rs.GiveBack(addr, false)
			assert.Equal(t, addr, rs.GetUnusedAddr(nil, 2))
		})
	}
}

func TestRegions_GetUnusedAddr_OneAddrLeft(t *testing.T) {
	tests := []struct {
		name  string
		addrs []*EdgeAddr
		mode  ConfigIPVersion
	}{
		{
			name:  "IPv4 addresses with IPv4Only",
			addrs: v4Addrs,
			mode:  IPv4Only,
		},
		{
			name:  "IPv6 addresses with IPv6Only",
			addrs: v6Addrs,
			mode:  IPv6Only,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := makeRegions(tt.addrs, tt.mode)
			rs.region1.AssignAnyAddress(0, nil)
			rs.region1.AssignAnyAddress(1, nil)
			rs.region2.AssignAnyAddress(2, nil)
			addr := rs.region2.active.GetUnusedIP(nil)

			assert.Equal(t, 1, rs.AvailableAddrs())
			assert.Equal(t, addr, rs.GetUnusedAddr(nil, 3))
		})
	}
}

func TestRegions_GetUnusedAddr_Excluding_Region1(t *testing.T) {
	tests := []struct {
		name  string
		addrs []*EdgeAddr
		mode  ConfigIPVersion
	}{
		{
			name:  "IPv4 addresses with IPv4Only",
			addrs: v4Addrs,
			mode:  IPv4Only,
		},
		{
			name:  "IPv6 addresses with IPv6Only",
			addrs: v6Addrs,
			mode:  IPv6Only,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := makeRegions(tt.addrs, tt.mode)

			rs.region1.AssignAnyAddress(0, nil)
			rs.region1.AssignAnyAddress(1, nil)
			addr := rs.region2.active.GetUnusedIP(nil)
			a2 := rs.region2.active.GetUnusedIP(addr)

			assert.Equal(t, 2, rs.AvailableAddrs())
			assert.Equal(t, addr, rs.GetUnusedAddr(a2, 3))
		})
	}
}

func TestRegions_GetUnusedAddr_Excluding_Region2(t *testing.T) {
	tests := []struct {
		name  string
		addrs []*EdgeAddr
		mode  ConfigIPVersion
	}{
		{
			name:  "IPv4 addresses with IPv4Only",
			addrs: v4Addrs,
			mode:  IPv4Only,
		},
		{
			name:  "IPv6 addresses with IPv6Only",
			addrs: v6Addrs,
			mode:  IPv6Only,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := makeRegions(tt.addrs, tt.mode)

			rs.region2.AssignAnyAddress(0, nil)
			rs.region2.AssignAnyAddress(1, nil)
			addr := rs.region1.active.GetUnusedIP(nil)
			a2 := rs.region1.active.GetUnusedIP(addr)

			assert.Equal(t, 2, rs.AvailableAddrs())
			assert.Equal(t, addr, rs.GetUnusedAddr(a2, 1))
		})
	}
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

func TestGetRegionalServiceName(t *testing.T) {
	// Empty region should just go to origintunneld
	globalServiceName := getRegionalServiceName("")
	assert.Equal(t, srvService, globalServiceName)

	// Non-empty region should go to the regional origintunneld variant
	for _, region := range []string{"us", "pt", "am"} {
		regionalServiceName := getRegionalServiceName(region)
		assert.Equal(t, region+"-"+srvService, regionalServiceName)
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
