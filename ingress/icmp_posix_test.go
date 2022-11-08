//go:build darwin || linux

package ingress

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/cloudflare/cloudflared/packet"
)

func TestFunnelIdleTimeout(t *testing.T) {
	const (
		idleTimeout = time.Second
		echoID      = 42573
		startSeq    = 8129
	)
	logger := zerolog.New(os.Stderr)
	proxy, err := newICMPProxy(localhostIP, "", &logger, idleTimeout)
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
	funnelID := flow3Tuple{
		srcIP:          pk.Src,
		dstIP:          pk.Dst,
		originalEchoID: echoID,
	}
	muxer := newMockMuxer(0)
	responder := packetResponder{
		datagramMuxer: muxer,
	}
	require.NoError(t, proxy.Request(ctx, &pk, &responder))
	validateEchoFlow(t, <-muxer.cfdToEdge, &pk)
	funnel1, found := proxy.srcFunnelTracker.Get(funnelID)
	require.True(t, found)

	// Send second request, should reuse the funnel
	require.NoError(t, proxy.Request(ctx, &pk, &packetResponder{
		datagramMuxer: muxer,
	}))
	validateEchoFlow(t, <-muxer.cfdToEdge, &pk)
	funnel2, found := proxy.srcFunnelTracker.Get(funnelID)
	require.True(t, found)
	require.Equal(t, funnel1, funnel2)

	time.Sleep(idleTimeout * 2)
	newMuxer := newMockMuxer(0)
	newResponder := packetResponder{
		datagramMuxer: newMuxer,
	}
	require.NoError(t, proxy.Request(ctx, &pk, &newResponder))
	validateEchoFlow(t, <-newMuxer.cfdToEdge, &pk)

	cancel()
	<-proxyDone
}
