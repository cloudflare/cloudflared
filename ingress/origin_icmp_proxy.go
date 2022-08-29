package ingress

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
)

const (
	defaultCloseAfterIdle = time.Second * 15
	mtu                   = 1500
	icmpTimeoutMs         = 1000
)

var (
	errFlowInactive = fmt.Errorf("flow is inactive")
	errPacketNil    = fmt.Errorf("packet is nil")
)

// ICMPProxy sends ICMP messages and listens for their responses
type ICMPProxy interface {
	// Serve starts listening for responses to the requests until context is done
	Serve(ctx context.Context) error
	// Request sends an ICMP message
	Request(pk *packet.ICMP, responder packet.FlowResponder) error
}

func NewICMPProxy(listenIP netip.Addr, logger *zerolog.Logger) (ICMPProxy, error) {
	return newICMPProxy(listenIP, logger)
}

// Opens a non-privileged ICMP socket on Linux and Darwin
func newICMPConn(listenIP netip.Addr) (*icmp.PacketConn, error) {
	network := "udp6"
	if listenIP.Is4() {
		network = "udp4"
	}
	return icmp.ListenPacket(network, listenIP.String())
}

func getICMPEcho(pk *packet.ICMP) (*icmp.Echo, error) {
	echo, ok := pk.Message.Body.(*icmp.Echo)
	if !ok {
		return nil, fmt.Errorf("expect ICMP echo, got %s", pk.Type)
	}
	return echo, nil
}
