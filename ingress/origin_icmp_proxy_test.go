package ingress

import (
	"context"
	"fmt"
	"net/netip"
	"runtime"
	"testing"

	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"github.com/cloudflare/cloudflared/packet"
)

var (
	noopLogger  = zerolog.Nop()
	localhostIP = netip.MustParseAddr("127.0.0.1")
)

// TestICMPProxyEcho makes sure we can send ICMP echo via the Request method and receives response via the
// ListenResponse method
//
// Note: if this test fails on your device under Linux, then most likely you need to make sure that your user
// is allowed in ping_group_range. See the following gist for how to do that:
// https://github.com/ValentinBELYN/icmplib/blob/main/docs/6-use-icmplib-without-privileges.md
func TestICMPProxyEcho(t *testing.T) {
	onlyDarwinOrLinux(t)
	const (
		echoID = 36571
		endSeq = 100
	)

	proxy, err := NewICMPProxy(localhostIP, &noopLogger)
	require.NoError(t, err)

	proxyDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		proxy.Serve(ctx)
		close(proxyDone)
	}()

	responder := echoFlowResponder{
		decoder:  packet.NewICMPDecoder(),
		respChan: make(chan []byte),
	}

	ip := packet.IP{
		Src:      localhostIP,
		Dst:      localhostIP,
		Protocol: layers.IPProtocolICMPv4,
	}
	for i := 0; i < endSeq; i++ {
		pk := packet.ICMP{
			IP: &ip,
			Message: &icmp.Message{
				Type: ipv4.ICMPTypeEcho,
				Code: 0,
				Body: &icmp.Echo{
					ID:   echoID,
					Seq:  i,
					Data: []byte(fmt.Sprintf("icmp echo seq %d", i)),
				},
			},
		}
		require.NoError(t, proxy.Request(&pk, &responder))
		responder.validate(t, &pk)
	}
	cancel()
	<-proxyDone
}

// TestICMPProxyRejectNotEcho makes sure it rejects messages other than echo
func TestICMPProxyRejectNotEcho(t *testing.T) {
	onlyDarwinOrLinux(t)
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
	proxy, err := NewICMPProxy(localhostIP, &noopLogger)
	require.NoError(t, err)

	responder := echoFlowResponder{
		decoder:  packet.NewICMPDecoder(),
		respChan: make(chan []byte),
	}
	for _, m := range msgs {
		pk := packet.ICMP{
			IP: &packet.IP{
				Src:      localhostIP,
				Dst:      localhostIP,
				Protocol: layers.IPProtocolICMPv4,
			},
			Message: &m,
		}
		require.Error(t, proxy.Request(&pk, &responder))
	}
}

func onlyDarwinOrLinux(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Cannot create non-privileged datagram-oriented ICMP endpoint on Windows")
	}
}

type echoFlowResponder struct {
	decoder  *packet.ICMPDecoder
	respChan chan []byte
}

func (efr *echoFlowResponder) SendPacket(pk packet.RawPacket) error {
	copiedPacket := make([]byte, len(pk.Data))
	copy(copiedPacket, pk.Data)
	efr.respChan <- copiedPacket
	return nil
}

func (efr *echoFlowResponder) validate(t *testing.T, echoReq *packet.ICMP) {
	pk := <-efr.respChan
	decoded, err := efr.decoder.Decode(packet.RawPacket{Data: pk})
	require.NoError(t, err)
	require.Equal(t, decoded.Src, echoReq.Dst)
	require.Equal(t, decoded.Dst, echoReq.Src)
	require.Equal(t, echoReq.Protocol, decoded.Protocol)

	require.Equal(t, ipv4.ICMPTypeEchoReply, decoded.Type)
	require.Equal(t, 0, decoded.Code)
	require.NotZero(t, decoded.Checksum)
	require.Equal(t, echoReq.Body, decoded.Body)
}
