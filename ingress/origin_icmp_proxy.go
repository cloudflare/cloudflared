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
	// funnelIdleTimeout controls how long to wait to close a funnel without send/return
	funnelIdleTimeout = time.Second * 10
	mtu               = 1500
	// icmpRequestTimeoutMs controls how long to wait for a reply
	icmpRequestTimeoutMs = 1000
)

var (
	errPacketNil = fmt.Errorf("packet is nil")
)

// ICMPProxy sends ICMP messages and listens for their responses
type ICMPProxy interface {
	// Serve starts listening for responses to the requests until context is done
	Serve(ctx context.Context) error
	// Request sends an ICMP message
	Request(pk *packet.ICMP, responder packet.FunnelUniPipe) error
}

func NewICMPProxy(listenIP netip.Addr, logger *zerolog.Logger) (ICMPProxy, error) {
	return newICMPProxy(listenIP, logger, funnelIdleTimeout)
}

func getICMPEcho(msg *icmp.Message) (*icmp.Echo, error) {
	echo, ok := msg.Body.(*icmp.Echo)
	if !ok {
		return nil, fmt.Errorf("expect ICMP echo, got %s", msg.Type)
	}
	return echo, nil
}
