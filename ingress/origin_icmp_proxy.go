package ingress

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

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

type icmpRouter struct {
	ipv4Proxy *icmpProxy
	ipv6Proxy *icmpProxy
}

// NewICMPRouter doesn't return an error if either ipv4 proxy or ipv6 proxy can be created. The machine might only
// support one of them
func NewICMPRouter(ipv4Addr, ipv6Addr netip.Addr, ipv6Zone string, logger *zerolog.Logger) (*icmpRouter, error) {
	ipv4Proxy, ipv4Err := newICMPProxy(ipv4Addr, "", logger, funnelIdleTimeout)
	ipv6Proxy, ipv6Err := newICMPProxy(ipv6Addr, ipv6Zone, logger, funnelIdleTimeout)
	if ipv4Err != nil && ipv6Err != nil {
		return nil, fmt.Errorf("cannot create ICMPv4 proxy: %v nor ICMPv6 proxy: %v", ipv4Err, ipv6Err)
	}
	if ipv4Err != nil {
		logger.Debug().Err(ipv4Err).Msg("failed to create ICMPv4 proxy, only ICMPv6 proxy is created")
		ipv4Proxy = nil
	}
	if ipv6Err != nil {
		logger.Debug().Err(ipv6Err).Msg("failed to create ICMPv6 proxy, only ICMPv4 proxy is created")
		ipv6Proxy = nil
	}
	return &icmpRouter{
		ipv4Proxy: ipv4Proxy,
		ipv6Proxy: ipv6Proxy,
	}, nil
}

func (ir *icmpRouter) Serve(ctx context.Context) error {
	if ir.ipv4Proxy != nil && ir.ipv6Proxy != nil {
		errC := make(chan error, 2)
		go func() {
			errC <- ir.ipv4Proxy.Serve(ctx)
		}()
		go func() {
			errC <- ir.ipv6Proxy.Serve(ctx)
		}()
		return <-errC
	}
	if ir.ipv4Proxy != nil {
		return ir.ipv4Proxy.Serve(ctx)
	}
	if ir.ipv6Proxy != nil {
		return ir.ipv6Proxy.Serve(ctx)
	}
	return fmt.Errorf("ICMPv4 proxy and ICMPv6 proxy are both nil")
}

func (ir *icmpRouter) Request(pk *packet.ICMP, responder packet.FunnelUniPipe) error {
	if pk.Dst.Is4() {
		if ir.ipv4Proxy != nil {
			return ir.ipv4Proxy.Request(pk, responder)
		}
		return fmt.Errorf("ICMPv4 proxy was not instantiated")
	}
	if ir.ipv6Proxy != nil {
		return ir.ipv6Proxy.Request(pk, responder)
	}
	return fmt.Errorf("ICMPv6 proxy was not instantiated")
}

func getICMPEcho(msg *icmp.Message) (*icmp.Echo, error) {
	echo, ok := msg.Body.(*icmp.Echo)
	if !ok {
		return nil, fmt.Errorf("expect ICMP echo, got %s", msg.Type)
	}
	return echo, nil
}

func isEchoReply(msg *icmp.Message) bool {
	return msg.Type == ipv4.ICMPTypeEchoReply || msg.Type == ipv6.ICMPTypeEchoReply
}
