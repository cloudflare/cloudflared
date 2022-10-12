package packet

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var (
	noopLogger   = zerolog.Nop()
	packetConfig = &GlobalRouterConfig{
		ICMPRouter: &mockICMPRouter{},
		IPv4Src:    netip.MustParseAddr("172.16.0.1"),
		IPv6Src:    netip.MustParseAddr("fd51:2391:523:f4ee::1"),
	}
)

func TestRouterReturnTTLExceed(t *testing.T) {
	upstream := &mockUpstream{
		source: make(chan RawPacket),
	}
	returnPipe := &mockFunnelUniPipe{
		uniPipe: make(chan RawPacket),
	}
	routerEnabled := &routerEnabledChecker{}
	routerEnabled.set(true)
	router := NewRouter(packetConfig, upstream, returnPipe, &noopLogger, routerEnabled.isEnabled)
	ctx, cancel := context.WithCancel(context.Background())
	routerStopped := make(chan struct{})
	go func() {
		router.Serve(ctx)
		close(routerStopped)
	}()

	pk := ICMP{
		IP: &IP{
			Src:      netip.MustParseAddr("192.168.1.1"),
			Dst:      netip.MustParseAddr("10.0.0.1"),
			Protocol: layers.IPProtocolICMPv4,
			TTL:      1,
		},
		Message: &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   12481,
				Seq:  8036,
				Data: []byte("TTL exceed"),
			},
		},
	}
	assertTTLExceed(t, &pk, router.globalConfig.IPv4Src, upstream, returnPipe)
	pk = ICMP{
		IP: &IP{
			Src:      netip.MustParseAddr("fd51:2391:523:f4ee::1"),
			Dst:      netip.MustParseAddr("fd51:2391:697:f4ee::2"),
			Protocol: layers.IPProtocolICMPv6,
			TTL:      1,
		},
		Message: &icmp.Message{
			Type: ipv6.ICMPTypeEchoRequest,
			Code: 0,
			Body: &icmp.Echo{
				ID:   42583,
				Seq:  7039,
				Data: []byte("TTL exceed"),
			},
		},
	}
	assertTTLExceed(t, &pk, router.globalConfig.IPv6Src, upstream, returnPipe)

	cancel()
	<-routerStopped
}

func TestRouterCheckEnabled(t *testing.T) {
	upstream := &mockUpstream{
		source: make(chan RawPacket),
	}
	returnPipe := &mockFunnelUniPipe{
		uniPipe: make(chan RawPacket),
	}
	routerEnabled := &routerEnabledChecker{}
	router := NewRouter(packetConfig, upstream, returnPipe, &noopLogger, routerEnabled.isEnabled)
	ctx, cancel := context.WithCancel(context.Background())
	routerStopped := make(chan struct{})
	go func() {
		router.Serve(ctx)
		close(routerStopped)
	}()

	pk := ICMP{
		IP: &IP{
			Src:      netip.MustParseAddr("192.168.1.1"),
			Dst:      netip.MustParseAddr("10.0.0.1"),
			Protocol: layers.IPProtocolICMPv4,
			TTL:      1,
		},
		Message: &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   12481,
				Seq:  8036,
				Data: []byte(t.Name()),
			},
		},
	}

	// router is disabled
	require.NoError(t, upstream.send(&pk))
	select {
	case <-time.After(time.Millisecond * 10):
	case <-returnPipe.uniPipe:
		t.Error("Unexpected reply when router is disabled")
	}
	routerEnabled.set(true)
	// router is enabled, expects reply
	require.NoError(t, upstream.send(&pk))
	<-returnPipe.uniPipe

	routerEnabled.set(false)
	// router is disabled
	require.NoError(t, upstream.send(&pk))
	select {
	case <-time.After(time.Millisecond * 10):
	case <-returnPipe.uniPipe:
		t.Error("Unexpected reply when router is disabled")
	}

	cancel()
	<-routerStopped
}

func assertTTLExceed(t *testing.T, originalPacket *ICMP, expectedSrc netip.Addr, upstream *mockUpstream, returnPipe *mockFunnelUniPipe) {
	encoder := NewEncoder()
	rawPacket, err := encoder.Encode(originalPacket)
	require.NoError(t, err)
	upstream.source <- rawPacket

	resp := <-returnPipe.uniPipe
	decoder := NewICMPDecoder()
	decoded, err := decoder.Decode(resp)
	require.NoError(t, err)

	require.Equal(t, expectedSrc, decoded.Src)
	require.Equal(t, originalPacket.Src, decoded.Dst)
	require.Equal(t, originalPacket.Protocol, decoded.Protocol)
	require.Equal(t, DefaultTTL, decoded.TTL)
	if originalPacket.Dst.Is4() {
		require.Equal(t, ipv4.ICMPTypeTimeExceeded, decoded.Type)
	} else {
		require.Equal(t, ipv6.ICMPTypeTimeExceeded, decoded.Type)
	}
	require.Equal(t, 0, decoded.Code)
	assertICMPChecksum(t, decoded)
	timeExceed, ok := decoded.Body.(*icmp.TimeExceeded)
	require.True(t, ok)
	require.True(t, bytes.Equal(rawPacket.Data, timeExceed.Data))
}

type mockUpstream struct {
	source chan RawPacket
}

func (ms *mockUpstream) send(pk Packet) error {
	encoder := NewEncoder()
	rawPacket, err := encoder.Encode(pk)
	if err != nil {
		return err
	}
	ms.source <- rawPacket
	return nil
}

func (ms *mockUpstream) ReceivePacket(ctx context.Context) (RawPacket, error) {
	select {
	case <-ctx.Done():
		return RawPacket{}, ctx.Err()
	case pk := <-ms.source:
		return pk, nil
	}
}

type mockICMPRouter struct{}

func (mir mockICMPRouter) Serve(ctx context.Context) error {
	return fmt.Errorf("Serve not implemented by mockICMPRouter")
}

func (mir mockICMPRouter) Request(pk *ICMP, responder FunnelUniPipe) error {
	return fmt.Errorf("Request not implemented by mockICMPRouter")
}

type routerEnabledChecker struct {
	enabled uint32
}

func (rec *routerEnabledChecker) isEnabled() bool {
	if atomic.LoadUint32(&rec.enabled) == 0 {
		return false
	}
	return true
}

func (rec *routerEnabledChecker) set(enabled bool) {
	if enabled {
		atomic.StoreUint32(&rec.enabled, 1)
	} else {
		atomic.StoreUint32(&rec.enabled, 0)
	}
}
