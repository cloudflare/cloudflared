//go:build darwin || linux

package ingress

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/cloudflare/cloudflared/packet"
)

func TestReturnToSrcUsesReplyTTL(t *testing.T) {
	t.Parallel()

	const originalEchoID = 42573
	muxer := newMockMuxer(1)
	responder := newPacketResponder(muxer, 0, packet.NewEncoder())
	flow := newICMPEchoFlow(localhostIP, func() error { return nil }, nil, responder, 0, originalEchoID)

	sent, err := flow.returnToSrc(&echoReply{
		from: localhostIP,
		msg: &icmp.Message{
			Type: ipv4.ICMPTypeEchoReply,
			Code: 0,
		},
		echo: &icmp.Echo{
			ID:   12345,
			Seq:  6789,
			Data: []byte(t.Name()),
		},
		ttl: receivedTTLFromIPHeader(42),
	})
	require.NoError(t, err)
	require.True(t, sent)

	resp := <-muxer.cfdToEdge
	decoder := packet.NewICMPDecoder()
	decoded, err := decoder.Decode(packet.RawPacket{Data: resp.Payload()})
	require.NoError(t, err)
	require.Equal(t, uint8(41), decoded.TTL)
	require.Equal(t, localhostIP, decoded.Src)
	require.Equal(t, localhostIP, decoded.Dst)
	require.Equal(t, ipv4.ICMPTypeEchoReply, decoded.Type)
	require.Equal(t, &icmp.Echo{
		ID:   originalEchoID,
		Seq:  6789,
		Data: []byte(t.Name()),
	}, decoded.Body)
}

func TestReturnToSrcDropsExpiredReplyTTL(t *testing.T) {
	t.Parallel()

	muxer := newMockMuxer(1)
	responder := newPacketResponder(muxer, 0, packet.NewEncoder())
	flow := newICMPEchoFlow(localhostIP, func() error { return nil }, nil, responder, 0, 42573)

	sent, err := flow.returnToSrc(&echoReply{
		from: localhostIP,
		msg: &icmp.Message{
			Type: ipv4.ICMPTypeEchoReply,
			Code: 0,
		},
		echo: &icmp.Echo{
			ID:   12345,
			Seq:  6789,
			Data: []byte(t.Name()),
		},
		ttl: receivedTTLFromIPHeader(1),
	})
	require.NoError(t, err)
	require.False(t, sent)

	select {
	case pk := <-muxer.cfdToEdge:
		t.Fatalf("received unexpected ICMP reply: %+v", pk)
	default:
	}
}

func TestFunnelIdleTimeout(t *testing.T) {
	defer leaktest.Check(t)()

	const (
		idleTimeout = time.Second
		echoID      = 42573
		startSeq    = 8129
	)
	logger := zerolog.New(os.Stderr)
	proxy, err := newICMPProxy(localhostIP, &logger, idleTimeout)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	proxyDone := make(chan struct{})
	go func() {
		proxy.Serve(ctx)
		close(proxyDone)
	}()

	// Send a packet to register the flow
	pk := packet.ICMP{
		IP: &packet.IP{
			Src:      localhostIP,
			Dst:      localhostIP,
			Protocol: layers.IPProtocolICMPv4,
		},
		Message: &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   echoID,
				Seq:  startSeq,
				Data: []byte(t.Name()),
			},
		},
	}
	muxer := newMockMuxer(0)
	responder := newPacketResponder(muxer, 0, packet.NewEncoder())
	require.NoError(t, proxy.Request(ctx, &pk, responder))
	validateEchoFlow(t, <-muxer.cfdToEdge, &pk)

	// Send second request, should reuse the funnel
	require.NoError(t, proxy.Request(ctx, &pk, responder))
	validateEchoFlow(t, <-muxer.cfdToEdge, &pk)

	// New muxer on a different connection should use a new flow
	time.Sleep(idleTimeout * 2)
	newMuxer := newMockMuxer(0)
	newResponder := newPacketResponder(newMuxer, 1, packet.NewEncoder())
	require.NoError(t, proxy.Request(ctx, &pk, newResponder))
	validateEchoFlow(t, <-newMuxer.cfdToEdge, &pk)

	time.Sleep(idleTimeout * 2)
	cancel()
	<-proxyDone
}

func TestReuseFunnel(t *testing.T) {
	defer leaktest.Check(t)()

	const (
		idleTimeout = time.Millisecond * 100
		echoID      = 42573
		startSeq    = 8129
	)
	logger := zerolog.New(os.Stderr)
	proxy, err := newICMPProxy(localhostIP, &logger, idleTimeout)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	proxyDone := make(chan struct{})
	go func() {
		proxy.Serve(ctx)
		close(proxyDone)
	}()

	// Send a packet to register the flow
	pk := packet.ICMP{
		IP: &packet.IP{
			Src:      localhostIP,
			Dst:      localhostIP,
			Protocol: layers.IPProtocolICMPv4,
		},
		Message: &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   echoID,
				Seq:  startSeq,
				Data: []byte(t.Name()),
			},
		},
	}
	tuple := flow3Tuple{
		srcIP:          pk.Src,
		dstIP:          pk.Dst,
		originalEchoID: echoID,
	}
	muxer := newMockMuxer(0)
	responder := newPacketResponder(muxer, 0, packet.NewEncoder())
	require.NoError(t, proxy.Request(ctx, &pk, responder))
	validateEchoFlow(t, <-muxer.cfdToEdge, &pk)
	funnel1, found := getFunnel(t, proxy, tuple)
	require.True(t, found)

	// Send second request, should reuse the funnel
	require.NoError(t, proxy.Request(ctx, &pk, responder))
	validateEchoFlow(t, <-muxer.cfdToEdge, &pk)
	funnel2, found := getFunnel(t, proxy, tuple)
	require.True(t, found)
	require.Equal(t, funnel1, funnel2)

	time.Sleep(idleTimeout * 2)

	cancel()
	<-proxyDone
}
