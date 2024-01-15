package ingress

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/cloudflare/cloudflared/packet"
	quicpogs "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/tracing"
)

var (
	noopLogger            = zerolog.Nop()
	localhostIP           = netip.MustParseAddr("127.0.0.1")
	localhostIPv6         = netip.MustParseAddr("::1")
	testFunnelIdleTimeout = time.Millisecond * 10
)

// TestICMPProxyEcho makes sure we can send ICMP echo via the Request method and receives response via the
// ListenResponse method
//
// Note: if this test fails on your device under Linux, then most likely you need to make sure that your user
// is allowed in ping_group_range. See the following gist for how to do that:
// https://github.com/ValentinBELYN/icmplib/blob/main/docs/6-use-icmplib-without-privileges.md
func TestICMPRouterEcho(t *testing.T) {
	testICMPRouterEcho(t, true)
	testICMPRouterEcho(t, false)
}

func testICMPRouterEcho(t *testing.T, sendIPv4 bool) {
	defer leaktest.Check(t)()

	const (
		echoID = 36571
		endSeq = 20
	)

	router, err := NewICMPRouter(localhostIP, localhostIPv6, "", &noopLogger, testFunnelIdleTimeout)
	require.NoError(t, err)

	proxyDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		router.Serve(ctx)
		close(proxyDone)
	}()

	muxer := newMockMuxer(1)
	responder := packetResponder{
		datagramMuxer: muxer,
	}

	protocol := layers.IPProtocolICMPv6
	if sendIPv4 {
		protocol = layers.IPProtocolICMPv4
	}
	localIPs := getLocalIPs(t, sendIPv4)
	ips := make([]*packet.IP, len(localIPs))
	for i, localIP := range localIPs {
		ips[i] = &packet.IP{
			Src:      localIP,
			Dst:      localIP,
			Protocol: protocol,
			TTL:      packet.DefaultTTL,
		}
	}

	var icmpType icmp.Type = ipv6.ICMPTypeEchoRequest
	if sendIPv4 {
		icmpType = ipv4.ICMPTypeEcho
	}
	for seq := 0; seq < endSeq; seq++ {
		for i, ip := range ips {
			pk := packet.ICMP{
				IP: ip,
				Message: &icmp.Message{
					Type: icmpType,
					Code: 0,
					Body: &icmp.Echo{
						ID:   echoID + i,
						Seq:  seq,
						Data: []byte(fmt.Sprintf("icmp echo seq %d", seq)),
					},
				},
			}
			require.NoError(t, router.Request(ctx, &pk, &responder))
			validateEchoFlow(t, <-muxer.cfdToEdge, &pk)
		}
	}

	// Make sure funnel cleanup kicks in
	time.Sleep(testFunnelIdleTimeout * 2)
	cancel()
	<-proxyDone
}

func TestTraceICMPRouterEcho(t *testing.T) {
	defer leaktest.Check(t)()

	tracingCtx := "ec31ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1"

	router, err := NewICMPRouter(localhostIP, localhostIPv6, "", &noopLogger, testFunnelIdleTimeout)
	require.NoError(t, err)

	proxyDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		router.Serve(ctx)
		close(proxyDone)
	}()

	// Buffer 3 packets, request span, reply span and reply
	muxer := newMockMuxer(3)
	tracingIdentity, err := tracing.NewIdentity(tracingCtx)
	require.NoError(t, err)
	serializedIdentity, err := tracingIdentity.MarshalBinary()
	require.NoError(t, err)

	responder := packetResponder{
		datagramMuxer:      muxer,
		tracedCtx:          tracing.NewTracedContext(ctx, tracingIdentity.String(), &noopLogger),
		serializedIdentity: serializedIdentity,
	}

	echo := &icmp.Echo{
		ID:   12910,
		Seq:  182,
		Data: []byte(t.Name()),
	}
	pk := packet.ICMP{
		IP: &packet.IP{
			Src:      localhostIP,
			Dst:      localhostIP,
			Protocol: layers.IPProtocolICMPv4,
			TTL:      packet.DefaultTTL,
		},
		Message: &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: echo,
		},
	}

	require.NoError(t, router.Request(ctx, &pk, &responder))
	firstPK := <-muxer.cfdToEdge
	var requestSpan *quicpogs.TracingSpanPacket
	// The order of receiving reply or request span is not deterministic
	switch firstPK.Type() {
	case quicpogs.DatagramTypeIP:
		// reply packet
		validateEchoFlow(t, firstPK, &pk)
	case quicpogs.DatagramTypeTracingSpan:
		// Request span
		requestSpan = firstPK.(*quicpogs.TracingSpanPacket)
		require.NotEmpty(t, requestSpan.Spans)
		require.True(t, bytes.Equal(serializedIdentity, requestSpan.TracingIdentity))
	default:
		panic(fmt.Sprintf("received unexpected packet type %d", firstPK.Type()))
	}

	secondPK := <-muxer.cfdToEdge
	if requestSpan != nil {
		// If first packet is request span, second packet should be the reply
		validateEchoFlow(t, secondPK, &pk)
	} else {
		requestSpan = secondPK.(*quicpogs.TracingSpanPacket)
		require.NotEmpty(t, requestSpan.Spans)
		require.True(t, bytes.Equal(serializedIdentity, requestSpan.TracingIdentity))
	}

	// Reply span
	thirdPacket := <-muxer.cfdToEdge
	replySpan, ok := thirdPacket.(*quicpogs.TracingSpanPacket)
	require.True(t, ok)
	require.NotEmpty(t, replySpan.Spans)
	require.True(t, bytes.Equal(serializedIdentity, replySpan.TracingIdentity))
	require.False(t, bytes.Equal(requestSpan.Spans, replySpan.Spans))

	echo.Seq++
	pk.Body = echo
	// Only first request for a flow is traced. The edge will not send tracing context for the second request
	newResponder := packetResponder{
		datagramMuxer: muxer,
	}
	require.NoError(t, router.Request(ctx, &pk, &newResponder))
	validateEchoFlow(t, <-muxer.cfdToEdge, &pk)

	select {
	case receivedPacket := <-muxer.cfdToEdge:
		panic(fmt.Sprintf("Receive unexpected packet %+v", receivedPacket))
	default:
	}

	time.Sleep(testFunnelIdleTimeout * 2)
	cancel()
	<-proxyDone
}

// TestConcurrentRequests makes sure icmpRouter can send concurrent requests to the same destination with different
// echo ID. This simulates concurrent ping to the same destination.
func TestConcurrentRequestsToSameDst(t *testing.T) {
	defer leaktest.Check(t)()

	const (
		concurrentPings = 5
		endSeq          = 5
	)

	router, err := NewICMPRouter(localhostIP, localhostIPv6, "", &noopLogger, testFunnelIdleTimeout)
	require.NoError(t, err)

	proxyDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		router.Serve(ctx)
		close(proxyDone)
	}()

	var wg sync.WaitGroup
	// icmpv4 and icmpv6 each has concurrentPings
	wg.Add(concurrentPings * 2)
	for i := 0; i < concurrentPings; i++ {
		echoID := 38451 + i
		go func() {
			defer wg.Done()

			muxer := newMockMuxer(1)
			responder := packetResponder{
				datagramMuxer: muxer,
			}
			for seq := 0; seq < endSeq; seq++ {
				pk := &packet.ICMP{
					IP: &packet.IP{
						Src:      localhostIP,
						Dst:      localhostIP,
						Protocol: layers.IPProtocolICMPv4,
						TTL:      packet.DefaultTTL,
					},
					Message: &icmp.Message{
						Type: ipv4.ICMPTypeEcho,
						Code: 0,
						Body: &icmp.Echo{
							ID:   echoID,
							Seq:  seq,
							Data: []byte(fmt.Sprintf("icmpv4 echo id %d, seq %d", echoID, seq)),
						},
					},
				}
				require.NoError(t, router.Request(ctx, pk, &responder))
				validateEchoFlow(t, <-muxer.cfdToEdge, pk)
			}
		}()
		go func() {
			defer wg.Done()
			muxer := newMockMuxer(1)
			responder := packetResponder{
				datagramMuxer: muxer,
			}
			for seq := 0; seq < endSeq; seq++ {
				pk := &packet.ICMP{
					IP: &packet.IP{
						Src:      localhostIPv6,
						Dst:      localhostIPv6,
						Protocol: layers.IPProtocolICMPv6,
						TTL:      packet.DefaultTTL,
					},
					Message: &icmp.Message{
						Type: ipv6.ICMPTypeEchoRequest,
						Code: 0,
						Body: &icmp.Echo{
							ID:   echoID,
							Seq:  seq,
							Data: []byte(fmt.Sprintf("icmpv6 echo id %d, seq %d", echoID, seq)),
						},
					},
				}
				require.NoError(t, router.Request(ctx, pk, &responder))
				validateEchoFlow(t, <-muxer.cfdToEdge, pk)
			}
		}()
	}
	wg.Wait()

	time.Sleep(testFunnelIdleTimeout * 2)
	cancel()
	<-proxyDone
}

// TestICMPProxyRejectNotEcho makes sure it rejects messages other than echo
func TestICMPRouterRejectNotEcho(t *testing.T) {
	defer leaktest.Check(t)()

	msgs := []icmp.Message{
		{
			Type: ipv4.ICMPTypeDestinationUnreachable,
			Code: 1,
			Body: &icmp.DstUnreach{
				Data: []byte("original packet"),
			},
		},
		{
			Type: ipv4.ICMPTypeTimeExceeded,
			Code: 1,
			Body: &icmp.TimeExceeded{
				Data: []byte("original packet"),
			},
		},
		{
			Type: ipv4.ICMPType(2),
			Code: 0,
			Body: &icmp.PacketTooBig{
				MTU:  1280,
				Data: []byte("original packet"),
			},
		},
	}
	testICMPRouterRejectNotEcho(t, localhostIP, msgs)
	msgsV6 := []icmp.Message{
		{
			Type: ipv6.ICMPTypeDestinationUnreachable,
			Code: 3,
			Body: &icmp.DstUnreach{
				Data: []byte("original packet"),
			},
		},
		{
			Type: ipv6.ICMPTypeTimeExceeded,
			Code: 0,
			Body: &icmp.TimeExceeded{
				Data: []byte("original packet"),
			},
		},
		{
			Type: ipv6.ICMPTypePacketTooBig,
			Code: 0,
			Body: &icmp.PacketTooBig{
				MTU:  1280,
				Data: []byte("original packet"),
			},
		},
	}
	testICMPRouterRejectNotEcho(t, localhostIPv6, msgsV6)
}

func testICMPRouterRejectNotEcho(t *testing.T, srcDstIP netip.Addr, msgs []icmp.Message) {
	router, err := NewICMPRouter(localhostIP, localhostIPv6, "", &noopLogger, testFunnelIdleTimeout)
	require.NoError(t, err)

	muxer := newMockMuxer(1)
	responder := packetResponder{
		datagramMuxer: muxer,
	}
	protocol := layers.IPProtocolICMPv4
	if srcDstIP.Is6() {
		protocol = layers.IPProtocolICMPv6
	}
	for _, m := range msgs {
		pk := packet.ICMP{
			IP: &packet.IP{
				Src:      srcDstIP,
				Dst:      srcDstIP,
				Protocol: protocol,
				TTL:      packet.DefaultTTL,
			},
			Message: &m,
		}
		require.Error(t, router.Request(context.Background(), &pk, &responder))
	}
}

func validateEchoFlow(t *testing.T, pk quicpogs.Packet, echoReq *packet.ICMP) {
	decoder := packet.NewICMPDecoder()
	decoded, err := decoder.Decode(packet.RawPacket{Data: pk.Payload()})
	require.NoError(t, err, pk)
	require.Equal(t, decoded.Src, echoReq.Dst)
	require.Equal(t, decoded.Dst, echoReq.Src)
	require.Equal(t, echoReq.Protocol, decoded.Protocol)

	if echoReq.Type == ipv4.ICMPTypeEcho {
		require.Equal(t, ipv4.ICMPTypeEchoReply, decoded.Type)
	} else {
		require.Equal(t, ipv6.ICMPTypeEchoReply, decoded.Type)
	}
	require.Equal(t, 0, decoded.Code)
	require.NotZero(t, decoded.Checksum)

	require.Equal(t, echoReq.Body, decoded.Body)
}

func getLocalIPs(t *testing.T, ipv4 bool) []netip.Addr {
	interfaces, err := net.Interfaces()
	require.NoError(t, err)
	localIPs := []netip.Addr{}
	for _, i := range interfaces {
		// Skip TUN devices, and Docker Networks
		if strings.Contains(i.Name, "tun") || strings.Contains(i.Name, "docker") || strings.HasPrefix(i.Name, "br-") {
			continue
		}
		addrs, err := i.Addrs()
		require.NoError(t, err)
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && (ipnet.IP.IsPrivate() || ipnet.IP.IsLoopback()) {
				// TODO DEVTOOLS-12514: We only run the IPv6 against the loopback interface due to issues on the CI runners.
				if (ipv4 && ipnet.IP.To4() != nil) || (!ipv4 && ipnet.IP.To4() == nil && ipnet.IP.IsLoopback()) {
					localIPs = append(localIPs, netip.MustParseAddr(ipnet.IP.String()))
				}
			}
		}
	}
	return localIPs
}
