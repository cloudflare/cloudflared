package ingress

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"

	"github.com/cloudflare/cloudflared/packet"
	"github.com/cloudflare/cloudflared/tracing"
)

const (
	mtu = 1500
	// icmpRequestTimeoutMs controls how long to wait for a reply
	icmpRequestTimeoutMs = 1000
)

var (
	errPacketNil = fmt.Errorf("packet is nil")
)

// ICMPRouterServer is a parent interface over-top of ICMPRouter that allows for the operation of the proxy origin listeners.
type ICMPRouterServer interface {
	ICMPRouter
	// Serve runs the ICMPRouter proxy origin listeners for any of the IPv4 or IPv6 interfaces configured.
	Serve(ctx context.Context) error
}

// ICMPRouter manages out-going ICMP requests towards the origin.
type ICMPRouter interface {
	// Request will send an ICMP packet towards the origin with an ICMPResponder to attach to the ICMP flow for the
	// response to utilize.
	Request(ctx context.Context, pk *packet.ICMP, responder ICMPResponder) error
	// ConvertToTTLExceeded will take an ICMP packet and create a ICMP TTL Exceeded response origininating from the
	// ICMPRouter's IP interface.
	ConvertToTTLExceeded(pk *packet.ICMP, rawPacket packet.RawPacket) *packet.ICMP
}

// ICMPResponder manages how to handle incoming ICMP messages coming from the origin to the edge.
type ICMPResponder interface {
	ConnectionIndex() uint8
	ReturnPacket(pk *packet.ICMP) error
	AddTraceContext(tracedCtx *tracing.TracedContext, serializedIdentity []byte)
	RequestSpan(ctx context.Context, pk *packet.ICMP) (context.Context, trace.Span)
	ReplySpan(ctx context.Context, logger *zerolog.Logger) (context.Context, trace.Span)
	ExportSpan()
}

type icmpRouter struct {
	ipv4Proxy *icmpProxy
	ipv4Src   netip.Addr
	ipv6Proxy *icmpProxy
	ipv6Src   netip.Addr
}

// NewICMPRouter doesn't return an error if either ipv4 proxy or ipv6 proxy can be created. The machine might only
// support one of them.
// funnelIdleTimeout controls how long to wait to close a funnel without send/return
func NewICMPRouter(ipv4Addr, ipv6Addr netip.Addr, logger *zerolog.Logger, funnelIdleTimeout time.Duration) (ICMPRouterServer, error) {
	ipv4Proxy, ipv4Err := newICMPProxy(ipv4Addr, logger, funnelIdleTimeout)
	ipv6Proxy, ipv6Err := newICMPProxy(ipv6Addr, logger, funnelIdleTimeout)
	if ipv4Err != nil && ipv6Err != nil {
		err := fmt.Errorf("cannot create ICMPv4 proxy: %v nor ICMPv6 proxy: %v", ipv4Err, ipv6Err)
		logger.Debug().Err(err).Msg("ICMP proxy feature is disabled")
		return nil, err
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
		ipv4Src:   ipv4Addr,
		ipv6Proxy: ipv6Proxy,
		ipv6Src:   ipv6Addr,
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

func (ir *icmpRouter) Request(ctx context.Context, pk *packet.ICMP, responder ICMPResponder) error {
	if pk == nil {
		return errPacketNil
	}
	if pk.Dst.Is4() {
		if ir.ipv4Proxy != nil {
			return ir.ipv4Proxy.Request(ctx, pk, responder)
		}
		return fmt.Errorf("ICMPv4 proxy was not instantiated")
	}
	if ir.ipv6Proxy != nil {
		return ir.ipv6Proxy.Request(ctx, pk, responder)
	}
	return fmt.Errorf("ICMPv6 proxy was not instantiated")
}

func (ir *icmpRouter) ConvertToTTLExceeded(pk *packet.ICMP, rawPacket packet.RawPacket) *packet.ICMP {
	var srcIP netip.Addr
	if pk.Dst.Is4() {
		srcIP = ir.ipv4Src
	} else {
		srcIP = ir.ipv6Src
	}
	return packet.NewICMPTTLExceedPacket(pk.IP, rawPacket, srcIP)
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

func observeICMPRequest(logger *zerolog.Logger, span trace.Span, src string, dst string, echoID int, seq int) {
	incrementICMPRequest()
	logger.Debug().
		Str("src", src).
		Str("dst", dst).
		Int("originalEchoID", echoID).
		Int("originalEchoSeq", seq).
		Msg("Received ICMP request")
	span.SetAttributes(
		attribute.Int("originalEchoID", echoID),
		attribute.Int("seq", seq),
	)
}

func observeICMPReply(logger *zerolog.Logger, span trace.Span, dst string, echoID int, seq int) {
	incrementICMPReply()
	logger.Debug().Str("dst", dst).Int("echoID", echoID).Int("seq", seq).Msg("Sent ICMP reply to edge")
	span.SetAttributes(
		attribute.String("dst", dst),
		attribute.Int("echoID", echoID),
		attribute.Int("seq", seq),
	)
}
