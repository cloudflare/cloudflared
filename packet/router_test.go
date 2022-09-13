package packet

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"testing"

	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var (
	noopLogger = zerolog.Nop()
)

func TestRouterReturnTTLExceed(t *testing.T) {
	upstream := &mockUpstream{
		source: make(chan RawPacket),
	}
	returnPipe := &mockFunnelUniPipe{
		uniPipe: make(chan RawPacket),
	}
	router := NewRouter(upstream, returnPipe, &mockICMPRouter{}, &noopLogger)
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
	assertTTLExceed(t, &pk, router.ipv4Src, upstream, returnPipe)
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
	assertTTLExceed(t, &pk, router.ipv6Src, upstream, returnPipe)

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
	timeExceed, ok := decoded.Body.(*icmp.TimeExceeded)
	require.True(t, ok)
	require.True(t, bytes.Equal(rawPacket.Data, timeExceed.Data))
}

type mockUpstream struct {
	source chan RawPacket
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
