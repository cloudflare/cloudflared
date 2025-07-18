package origins

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestDNSResolver_DefaultResolver(t *testing.T) {
	log := zerolog.Nop()
	service := NewDNSResolverService(NewDNSDialer(), &log, &noopMetrics{})
	mockResolver := &mockPeekResolver{
		address: "127.0.0.2:53",
	}
	service.resolver = mockResolver
	validateAddrs(t, []netip.AddrPort{defaultResolverAddr}, service.addresses)
}

func TestStaticDNSResolver_DefaultResolver(t *testing.T) {
	log := zerolog.Nop()
	addresses := []netip.AddrPort{netip.MustParseAddrPort("1.1.1.1:53"), netip.MustParseAddrPort("1.0.0.1:53")}
	service := NewStaticDNSResolverService(addresses, NewDNSDialer(), &log, &noopMetrics{})
	mockResolver := &mockPeekResolver{
		address: "127.0.0.2:53",
	}
	service.resolver = mockResolver
	validateAddrs(t, addresses, service.addresses)
}

func TestDNSResolver_UpdateResolverAddress(t *testing.T) {
	log := zerolog.Nop()
	service := NewDNSResolverService(NewDNSDialer(), &log, &noopMetrics{})

	mockResolver := &mockPeekResolver{}
	service.resolver = mockResolver

	tests := []struct {
		addr     string
		expected netip.AddrPort
	}{
		{"127.0.0.2:53", netip.MustParseAddrPort("127.0.0.2:53")},
		// missing port should be added (even though this is unlikely to happen)
		{"127.0.0.3", netip.MustParseAddrPort("127.0.0.3:53")},
	}

	for _, test := range tests {
		mockResolver.address = test.addr
		// Update the resolver address
		err := service.update(t.Context())
		if err != nil {
			t.Error(err)
		}
		// Validate expected
		validateAddrs(t, []netip.AddrPort{test.expected}, service.addresses)
	}
}

func TestStaticDNSResolver_RefreshLoopExits(t *testing.T) {
	log := zerolog.Nop()
	addresses := []netip.AddrPort{netip.MustParseAddrPort("1.1.1.1:53"), netip.MustParseAddrPort("1.0.0.1:53")}
	service := NewStaticDNSResolverService(addresses, NewDNSDialer(), &log, &noopMetrics{})

	mockResolver := &mockPeekResolver{
		address: "127.0.0.2:53",
	}
	service.resolver = mockResolver

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go service.StartRefreshLoop(ctx)

	// Wait for the refresh loop to end _and_ not update the addresses
	time.Sleep(10 * time.Millisecond)

	// Validate expected
	validateAddrs(t, addresses, service.addresses)
}

func TestDNSResolver_UpdateResolverAddressInvalid(t *testing.T) {
	log := zerolog.Nop()
	service := NewDNSResolverService(NewDNSDialer(), &log, &noopMetrics{})
	mockResolver := &mockPeekResolver{}
	service.resolver = mockResolver

	invalidAddresses := []string{
		"999.999.999.999",
		"localhost",
		"255.255.255",
	}

	for _, addr := range invalidAddresses {
		mockResolver.address = addr
		// Update the resolver address should not update for these invalid addresses
		err := service.update(t.Context())
		if err == nil {
			t.Error("service update should throw an error")
		}
		// Validate expected
		validateAddrs(t, []netip.AddrPort{defaultResolverAddr}, service.addresses)
	}
}

func TestDNSResolver_UpdateResolverErrorIgnored(t *testing.T) {
	log := zerolog.Nop()
	service := NewDNSResolverService(NewDNSDialer(), &log, &noopMetrics{})
	resolverErr := errors.New("test resolver error")
	mockResolver := &mockPeekResolver{err: resolverErr}
	service.resolver = mockResolver

	// Update the resolver address should not update when the resolver cannot complete the lookup
	err := service.update(t.Context())
	if err != resolverErr {
		t.Error("service update should throw an error")
	}
	// Validate expected
	validateAddrs(t, []netip.AddrPort{defaultResolverAddr}, service.addresses)
}

func TestDNSResolver_DialUDPUsesResolvedAddress(t *testing.T) {
	log := zerolog.Nop()
	mockDialer := &mockDialer{expected: defaultResolverAddr}
	service := NewDNSResolverService(mockDialer, &log, &noopMetrics{})
	mockResolver := &mockPeekResolver{}
	service.resolver = mockResolver

	// Attempt a dial to 127.0.0.2:53 which should be ignored and instead resolve to 127.0.0.1:53
	_, err := service.DialUDP(netip.MustParseAddrPort("127.0.0.2:53"))
	if err != nil {
		t.Error(err)
	}
}

func TestDNSResolver_DialTCPUsesResolvedAddress(t *testing.T) {
	log := zerolog.Nop()
	mockDialer := &mockDialer{expected: defaultResolverAddr}
	service := NewDNSResolverService(mockDialer, &log, &noopMetrics{})
	mockResolver := &mockPeekResolver{}
	service.resolver = mockResolver

	// Attempt a dial to 127.0.0.2:53 which should be ignored and instead resolve to 127.0.0.1:53
	_, err := service.DialTCP(t.Context(), netip.MustParseAddrPort("127.0.0.2:53"))
	if err != nil {
		t.Error(err)
	}
}

type mockPeekResolver struct {
	err     error
	address string
}

func (r *mockPeekResolver) addr() (network, address string) {
	return "udp", r.address
}

func (r *mockPeekResolver) lookupNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	// We can return an empty result as it doesn't matter as long as the lookup doesn't fail
	return []netip.Addr{}, r.err
}

type mockDialer struct {
	expected netip.AddrPort
}

func (d *mockDialer) DialTCP(ctx context.Context, addr netip.AddrPort) (net.Conn, error) {
	if d.expected != addr {
		return nil, errors.New("unexpected address dialed")
	}
	return nil, nil
}

func (d *mockDialer) DialUDP(addr netip.AddrPort) (net.Conn, error) {
	if d.expected != addr {
		return nil, errors.New("unexpected address dialed")
	}
	return nil, nil
}

func validateAddrs(t *testing.T, expected []netip.AddrPort, actual []netip.AddrPort) {
	if len(actual) != len(expected) {
		t.Errorf("addresses should only contain one element: %s", actual)
	}
	for _, e := range expected {
		if !slices.Contains(actual, e) {
			t.Errorf("missing address: %s in %s", e, actual)
		}
	}
}
