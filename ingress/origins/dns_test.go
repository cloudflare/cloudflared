package origins

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/rs/zerolog"
)

func TestDNSResolver_DefaultResolver(t *testing.T) {
	log := zerolog.Nop()
	service := NewDNSResolver(&log)
	mockResolver := &mockPeekResolver{
		address: "127.0.0.2:53",
	}
	service.resolver = mockResolver
	if service.address != defaultResolverAddr {
		t.Errorf("resolver address should be the default: %s, was: %s", defaultResolverAddr, service.address)
	}
}

func TestDNSResolver_UpdateResolverAddress(t *testing.T) {
	log := zerolog.Nop()
	service := NewDNSResolver(&log)

	mockResolver := &mockPeekResolver{}
	service.resolver = mockResolver

	expectedAddr := netip.MustParseAddrPort("127.0.0.2:53")
	addresses := []string{
		"127.0.0.2:53",
		"127.0.0.2", // missing port should be added (even though this is unlikely to happen)
	}

	for _, addr := range addresses {
		mockResolver.address = addr
		// Update the resolver address
		err := service.update(t.Context())
		if err != nil {
			t.Error(err)
		}
		// Validate expected
		if service.address != expectedAddr {
			t.Errorf("resolver address should be: %s, was: %s", expectedAddr, service.address)
		}
	}
}

func TestDNSResolver_UpdateResolverAddressInvalid(t *testing.T) {
	log := zerolog.Nop()
	service := NewDNSResolver(&log)
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
		if service.address != defaultResolverAddr {
			t.Errorf("resolver address should not be updated from default: %s, was: %s", defaultResolverAddr, service.address)
		}
	}
}

func TestDNSResolver_UpdateResolverErrorIgnored(t *testing.T) {
	log := zerolog.Nop()
	service := NewDNSResolver(&log)
	resolverErr := errors.New("test resolver error")
	mockResolver := &mockPeekResolver{err: resolverErr}
	service.resolver = mockResolver

	// Update the resolver address should not update when the resolver cannot complete the lookup
	err := service.update(t.Context())
	if err != resolverErr {
		t.Error("service update should throw an error")
	}
	// Validate expected
	if service.address != defaultResolverAddr {
		t.Errorf("resolver address should not be updated from default: %s, was: %s", defaultResolverAddr, service.address)
	}
}

func TestDNSResolver_DialUsesResolvedAddress(t *testing.T) {
	log := zerolog.Nop()
	service := NewDNSResolver(&log)
	mockResolver := &mockPeekResolver{}
	service.resolver = mockResolver
	mockDialer := &mockDialer{expected: defaultResolverAddr}
	service.dialer = mockDialer

	// Attempt a dial to 127.0.0.2:53 which should be ignored and instead resolve to 127.0.0.1:53
	_, err := service.DialUDP(netip.MustParseAddrPort("127.0.0.2:53"))
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

func (d *mockDialer) DialUDP(addr netip.AddrPort) (net.Conn, error) {
	if d.expected != addr {
		return nil, errors.New("unexpected address dialed")
	}
	return nil, nil
}
