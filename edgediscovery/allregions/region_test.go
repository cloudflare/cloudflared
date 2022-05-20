package allregions

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func makeAddrSet(addrs []*EdgeAddr) AddrSet {
	addrSet := make(AddrSet, len(addrs))
	for _, addr := range addrs {
		addrSet[addr] = Unused()
	}
	return addrSet
}

func TestRegion_New(t *testing.T) {
	tests := []struct {
		name          string
		addrs         []*EdgeAddr
		mode          ConfigIPVersion
		expectedAddrs int
		primary       AddrSet
		secondary     AddrSet
	}{
		{
			name:          "IPv4 addresses with IPv4Only",
			addrs:         v4Addrs,
			mode:          IPv4Only,
			expectedAddrs: len(v4Addrs),
			primary:       makeAddrSet(v4Addrs),
			secondary:     AddrSet{},
		},
		{
			name:          "IPv6 addresses with IPv4Only",
			addrs:         v6Addrs,
			mode:          IPv4Only,
			expectedAddrs: 0,
			primary:       AddrSet{},
			secondary:     AddrSet{},
		},
		{
			name:          "IPv6 addresses with IPv6Only",
			addrs:         v6Addrs,
			mode:          IPv6Only,
			expectedAddrs: len(v6Addrs),
			primary:       makeAddrSet(v6Addrs),
			secondary:     AddrSet{},
		},
		{
			name:          "IPv6 addresses with IPv4Only",
			addrs:         v6Addrs,
			mode:          IPv4Only,
			expectedAddrs: 0,
			primary:       AddrSet{},
			secondary:     AddrSet{},
		},
		{
			name:          "IPv4 (first) and IPv6 addresses with Auto",
			addrs:         append(v4Addrs, v6Addrs...),
			mode:          Auto,
			expectedAddrs: len(v4Addrs),
			primary:       makeAddrSet(v4Addrs),
			secondary:     makeAddrSet(v6Addrs),
		},
		{
			name:          "IPv6 (first) and IPv4 addresses with Auto",
			addrs:         append(v6Addrs, v4Addrs...),
			mode:          Auto,
			expectedAddrs: len(v6Addrs),
			primary:       makeAddrSet(v6Addrs),
			secondary:     makeAddrSet(v4Addrs),
		},
		{
			name:          "IPv4 addresses with Auto",
			addrs:         v4Addrs,
			mode:          Auto,
			expectedAddrs: len(v4Addrs),
			primary:       makeAddrSet(v4Addrs),
			secondary:     AddrSet{},
		},
		{
			name:          "IPv6 addresses with Auto",
			addrs:         v6Addrs,
			mode:          Auto,
			expectedAddrs: len(v6Addrs),
			primary:       makeAddrSet(v6Addrs),
			secondary:     AddrSet{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegion(tt.addrs, tt.mode)
			assert.Equal(t, tt.expectedAddrs, r.AvailableAddrs())
			assert.Equal(t, tt.primary, r.primary)
			assert.Equal(t, tt.secondary, r.secondary)
		})
	}
}

func TestRegion_AnyAddress_EmptyActiveSet(t *testing.T) {
	tests := []struct {
		name  string
		addrs []*EdgeAddr
		mode  ConfigIPVersion
	}{
		{
			name:  "IPv6 addresses with IPv4Only",
			addrs: v6Addrs,
			mode:  IPv4Only,
		},
		{
			name:  "IPv4 addresses with IPv6Only",
			addrs: v4Addrs,
			mode:  IPv6Only,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegion(tt.addrs, tt.mode)
			addr := r.GetAnyAddress()
			assert.Nil(t, addr)
			addr = r.AssignAnyAddress(0, nil)
			assert.Nil(t, addr)
		})
	}
}

func TestRegion_AssignAnyAddress_FullyUsedActiveSet(t *testing.T) {
	tests := []struct {
		name  string
		addrs []*EdgeAddr
		mode  ConfigIPVersion
	}{
		{
			name:  "IPv6 addresses with IPv6Only",
			addrs: v6Addrs,
			mode:  IPv6Only,
		},
		{
			name:  "IPv4 addresses with IPv4Only",
			addrs: v4Addrs,
			mode:  IPv4Only,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegion(tt.addrs, tt.mode)
			total := r.active.AvailableAddrs()
			for i := 0; i < total; i++ {
				addr := r.AssignAnyAddress(i, nil)
				assert.NotNil(t, addr)
			}
			addr := r.AssignAnyAddress(9, nil)
			assert.Nil(t, addr)
		})
	}
}

var giveBackTests = []struct {
	name          string
	addrs         []*EdgeAddr
	mode          ConfigIPVersion
	expectedAddrs int
	primary       AddrSet
	secondary     AddrSet
	primarySwap   bool
}{
	{
		name:          "IPv4 addresses with IPv4Only",
		addrs:         v4Addrs,
		mode:          IPv4Only,
		expectedAddrs: len(v4Addrs),
		primary:       makeAddrSet(v4Addrs),
		secondary:     AddrSet{},
		primarySwap:   false,
	},
	{
		name:          "IPv6 addresses with IPv6Only",
		addrs:         v6Addrs,
		mode:          IPv6Only,
		expectedAddrs: len(v6Addrs),
		primary:       makeAddrSet(v6Addrs),
		secondary:     AddrSet{},
		primarySwap:   false,
	},
	{
		name:          "IPv4 (first) and IPv6 addresses with Auto",
		addrs:         append(v4Addrs, v6Addrs...),
		mode:          Auto,
		expectedAddrs: len(v4Addrs),
		primary:       makeAddrSet(v4Addrs),
		secondary:     makeAddrSet(v6Addrs),
		primarySwap:   false,
	},
	{
		name:          "IPv6 (first) and IPv4 addresses with Auto",
		addrs:         append(v6Addrs, v4Addrs...),
		mode:          Auto,
		expectedAddrs: len(v6Addrs),
		primary:       makeAddrSet(v6Addrs),
		secondary:     makeAddrSet(v4Addrs),
		primarySwap:   true,
	},
	{
		name:          "IPv4 addresses with Auto",
		addrs:         v4Addrs,
		mode:          Auto,
		expectedAddrs: len(v4Addrs),
		primary:       makeAddrSet(v4Addrs),
		secondary:     AddrSet{},
		primarySwap:   false,
	},
	{
		name:          "IPv6 addresses with Auto",
		addrs:         v6Addrs,
		mode:          Auto,
		expectedAddrs: len(v6Addrs),
		primary:       makeAddrSet(v6Addrs),
		secondary:     AddrSet{},
		primarySwap:   false,
	},
}

func TestRegion_GiveBack_NoConnectivityError(t *testing.T) {
	for _, tt := range giveBackTests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegion(tt.addrs, tt.mode)
			addr := r.AssignAnyAddress(0, nil)
			assert.NotNil(t, addr)
			assert.True(t, r.GiveBack(addr, false))
		})
	}
}

func TestRegion_GiveBack_ForeignAddr(t *testing.T) {
	invalid := EdgeAddr{
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
		IPVersion: V4,
	}
	for _, tt := range giveBackTests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegion(tt.addrs, tt.mode)
			assert.False(t, r.GiveBack(&invalid, false))
			assert.False(t, r.GiveBack(&invalid, true))
		})
	}
}

func TestRegion_GiveBack_SwapPrimary(t *testing.T) {
	for _, tt := range giveBackTests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegion(tt.addrs, tt.mode)
			addr := r.AssignAnyAddress(0, nil)
			assert.NotNil(t, addr)
			assert.True(t, r.GiveBack(addr, true))
			assert.Equal(t, tt.primarySwap, !r.primaryIsActive)
			if tt.primarySwap {
				assert.Equal(t, r.secondary, r.active)
				assert.False(t, r.primaryTimeout.IsZero())
			} else {
				assert.Equal(t, r.primary, r.active)
				assert.True(t, r.primaryTimeout.IsZero())
			}
		})
	}
}

func TestRegion_GiveBack_IPv4_ResetPrimary(t *testing.T) {
	r := NewRegion(append(v6Addrs, v4Addrs...), Auto)
	// Exhaust all IPv6 addresses
	a0 := r.AssignAnyAddress(0, nil)
	a1 := r.AssignAnyAddress(1, nil)
	a2 := r.AssignAnyAddress(2, nil)
	a3 := r.AssignAnyAddress(3, nil)
	assert.NotNil(t, a0)
	assert.NotNil(t, a1)
	assert.NotNil(t, a2)
	assert.NotNil(t, a3)
	// Give back the first IPv6 address to fallback to secondary IPv4 address set
	assert.True(t, r.GiveBack(a0, true))
	assert.False(t, r.primaryIsActive)
	// Give back another IPv6 address
	assert.True(t, r.GiveBack(a1, true))
	// Primary shouldn't change
	assert.False(t, r.primaryIsActive)
	// Request an address (should be IPv4 from secondary)
	a4_v4 := r.AssignAnyAddress(4, nil)
	assert.NotNil(t, a4_v4)
	assert.Equal(t, V4, a4_v4.IPVersion)
	a5_v4 := r.AssignAnyAddress(5, nil)
	assert.NotNil(t, a5_v4)
	assert.Equal(t, V4, a5_v4.IPVersion)
	a6_v4 := r.AssignAnyAddress(6, nil)
	assert.NotNil(t, a6_v4)
	assert.Equal(t, V4, a6_v4.IPVersion)
	// Return IPv4 address (without failure)
	// Primary shouldn't change because it is not a connectivity failure
	assert.True(t, r.GiveBack(a4_v4, false))
	assert.False(t, r.primaryIsActive)
	// Return IPv4 address (with failure)
	// Primary should change because it is a connectivity failure
	assert.True(t, r.GiveBack(a5_v4, true))
	assert.True(t, r.primaryIsActive)
	// Return IPv4 address (with failure)
	// Primary shouldn't change because the address is returned to the inactive
	// secondary address set
	assert.True(t, r.GiveBack(a6_v4, true))
	assert.True(t, r.primaryIsActive)
	// Return IPv6 address (without failure)
	// Primary shoudn't change because it is not a connectivity failure
	assert.True(t, r.GiveBack(a2, false))
	assert.True(t, r.primaryIsActive)
}

func TestRegion_GiveBack_Timeout(t *testing.T) {
	r := NewRegion(append(v6Addrs, v4Addrs...), Auto)
	a0 := r.AssignAnyAddress(0, nil)
	a1 := r.AssignAnyAddress(1, nil)
	a2 := r.AssignAnyAddress(2, nil)
	assert.NotNil(t, a0)
	assert.NotNil(t, a1)
	assert.NotNil(t, a2)
	// Give back IPv6 address to set timeout
	assert.True(t, r.GiveBack(a0, true))
	assert.False(t, r.primaryIsActive)
	assert.False(t, r.primaryTimeout.IsZero())
	// Request an address (should be IPv4 from secondary)
	a3_v4 := r.AssignAnyAddress(3, nil)
	assert.NotNil(t, a3_v4)
	assert.Equal(t, V4, a3_v4.IPVersion)
	assert.False(t, r.primaryIsActive)
	// Give back IPv6 address inside timeout (no change)
	assert.True(t, r.GiveBack(a2, true))
	assert.False(t, r.primaryIsActive)
	assert.False(t, r.primaryTimeout.IsZero())
	// Accelerate timeout
	r.primaryTimeout = time.Now().Add(-time.Minute)
	// Return IPv6 address
	assert.True(t, r.GiveBack(a1, true))
	assert.True(t, r.primaryIsActive)
	// Returning an IPv4 address after primary is active shouldn't change primary
	// even with a connectivity error
	assert.True(t, r.GiveBack(a3_v4, true))
	assert.True(t, r.primaryIsActive)
}
