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
	responder := echoFlowResponder{
		decoder:  packet.NewICMPDecoder(),
		respChan: make(chan []byte),
	}
	require.NoError(t, proxy.Request(&pk, &responder))
	responder.validate(t, &pk)

	// Send second request, should reuse the funnel
	require.NoError(t, proxy.Request(&pk, nil))
	responder.validate(t, &pk)

	time.Sleep(idleTimeout * 2)
	newResponder := echoFlowResponder{
		decoder:  packet.NewICMPDecoder(),
		respChan: make(chan []byte),
	}
	require.NoError(t, proxy.Request(&pk, &newResponder))
	newResponder.validate(t, &pk)

	cancel()
	<-proxyDone
}
