package ingress

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"sync/atomic"
	"testing"

	"github.com/google/gopacket/layers"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/cloudflare/cloudflared/packet"
	quicpogs "github.com/cloudflare/cloudflared/quic"
)

var (
	defaultRouter = &icmpRouter{
		ipv4Proxy: nil,
		ipv4Src:   netip.MustParseAddr("172.16.0.1"),
		ipv6Proxy: nil,
		ipv6Src:   netip.MustParseAddr("fd51:2391:523:f4ee::1"),
	}
)

func TestRouterReturnTTLExceed(t *testing.T) {
	muxer := newMockMuxer(0)
	router := NewPacketRouter(defaultRouter, muxer, 0, &noopLogger)
	ctx, cancel := context.WithCancel(context.Background())
	routerStopped := make(chan struct{})
	go func() {
		router.Serve(ctx)
		close(routerStopped)
	}()

	pk := packet.ICMP{
		IP: &packet.IP{
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
	assertTTLExceed(t, &pk, defaultRouter.ipv4Src, muxer)
	pk = packet.ICMP{
		IP: &packet.IP{
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
	assertTTLExceed(t, &pk, defaultRouter.ipv6Src, muxer)

	cancel()
	<-routerStopped
}

func assertTTLExceed(t *testing.T, originalPacket *packet.ICMP, expectedSrc netip.Addr, muxer *mockMuxer) {
	encoder := packet.NewEncoder()
	rawPacket, err := encoder.Encode(originalPacket)
	require.NoError(t, err)
	muxer.edgeToCfd <- quicpogs.RawPacket(rawPacket)

	resp := <-muxer.cfdToEdge
	decoder := packet.NewICMPDecoder()
	decoded, err := decoder.Decode(packet.RawPacket(resp.(quicpogs.RawPacket)))
	require.NoError(t, err)

	require.Equal(t, expectedSrc, decoded.Src)
	require.Equal(t, originalPacket.Src, decoded.Dst)
	require.Equal(t, originalPacket.Protocol, decoded.Protocol)
	require.Equal(t, packet.DefaultTTL, decoded.TTL)
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

type mockMuxer struct {
	cfdToEdge chan quicpogs.Packet
	edgeToCfd chan quicpogs.Packet
}

func newMockMuxer(capacity int) *mockMuxer {
	return &mockMuxer{
		cfdToEdge: make(chan quicpogs.Packet, capacity),
		edgeToCfd: make(chan quicpogs.Packet, capacity),
	}
}

// Copy packet, because icmpProxy expects the encoder buffer to be reusable after the packet is sent
func (mm *mockMuxer) SendPacket(pk quicpogs.Packet) error {
	payload := pk.Payload()
	copiedPayload := make([]byte, len(payload))
	copy(copiedPayload, payload)

	metadata := pk.Metadata()
	copiedMetadata := make([]byte, len(metadata))
	copy(copiedMetadata, metadata)

	var copiedPacket quicpogs.Packet
	switch pk.Type() {
	case quicpogs.DatagramTypeIP:
		copiedPacket = quicpogs.RawPacket(packet.RawPacket{
			Data: copiedPayload,
		})
	case quicpogs.DatagramTypeIPWithTrace:
		copiedPacket = &quicpogs.TracedPacket{
			Packet: packet.RawPacket{
				Data: copiedPayload,
			},
			TracingIdentity: copiedMetadata,
		}
	case quicpogs.DatagramTypeTracingSpan:
		copiedPacket = &quicpogs.TracingSpanPacket{
			Spans:           copiedPayload,
			TracingIdentity: copiedMetadata,
		}
	default:
		return fmt.Errorf("unexpected metadata type %d", pk.Type())
	}
	mm.cfdToEdge <- copiedPacket
	return nil
}

func (mm *mockMuxer) ReceivePacket(ctx context.Context) (quicpogs.Packet, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case pk := <-mm.edgeToCfd:
		return pk, nil
	}
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
