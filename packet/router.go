package packet

import (
	"context"
	"net/netip"

	"github.com/rs/zerolog"
)

// ICMPRouter sends ICMP messages and listens for their responses
type ICMPRouter interface {
	// Serve starts listening for responses to the requests until context is done
	Serve(ctx context.Context) error
	// Request sends an ICMP message
	Request(pk *ICMP, responder FunnelUniPipe) error
}

// Upstream of raw packets
type Upstream interface {
	// ReceivePacket waits for the next raw packet from upstream
	ReceivePacket(ctx context.Context) (RawPacket, error)
}

// Router routes packets between Upstream and ICMPRouter. Currently it rejects all other type of ICMP packets
type Router struct {
	upstream   Upstream
	returnPipe FunnelUniPipe
	icmpRouter ICMPRouter
	ipv4Src    netip.Addr
	ipv6Src    netip.Addr
	logger     *zerolog.Logger
}

// GlobalRouterConfig is the configuration shared by all instance of Router.
type GlobalRouterConfig struct {
	ICMPRouter ICMPRouter
	IPv4Src    netip.Addr
	IPv6Src    netip.Addr
	Zone       string
}

func NewRouter(globalConfig *GlobalRouterConfig, upstream Upstream, returnPipe FunnelUniPipe, logger *zerolog.Logger) *Router {
	return &Router{
		upstream:   upstream,
		returnPipe: returnPipe,
		icmpRouter: globalConfig.ICMPRouter,
		ipv4Src:    globalConfig.IPv4Src,
		ipv6Src:    globalConfig.IPv6Src,
		logger:     logger,
	}
}

func (r *Router) Serve(ctx context.Context) error {
	icmpDecoder := NewICMPDecoder()
	encoder := NewEncoder()
	for {
		rawPacket, err := r.upstream.ReceivePacket(ctx)
		if err != nil {
			return err
		}
		icmpPacket, err := icmpDecoder.Decode(rawPacket)
		if err != nil {
			r.logger.Err(err).Msg("Failed to decode ICMP packet from quic datagram")
			continue
		}

		if icmpPacket.TTL <= 1 {
			if err := r.sendTTLExceedMsg(icmpPacket, rawPacket, encoder); err != nil {
				r.logger.Err(err).Msg("Failed to return ICMP TTL exceed error")
			}
			continue
		}
		icmpPacket.TTL--

		if err := r.icmpRouter.Request(icmpPacket, r.returnPipe); err != nil {
			r.logger.Err(err).
				Str("src", icmpPacket.Src.String()).
				Str("dst", icmpPacket.Dst.String()).
				Interface("type", icmpPacket.Type).
				Msg("Failed to send ICMP packet")
			continue
		}
	}
}

func (r *Router) sendTTLExceedMsg(pk *ICMP, rawPacket RawPacket, encoder *Encoder) error {
	var srcIP netip.Addr
	if pk.Dst.Is4() {
		srcIP = r.ipv4Src
	} else {
		srcIP = r.ipv6Src
	}
	ttlExceedPacket := NewICMPTTLExceedPacket(pk.IP, rawPacket, srcIP)

	encodedTTLExceed, err := encoder.Encode(ttlExceedPacket)
	if err != nil {
		return err
	}
	return r.returnPipe.SendPacket(pk.Src, encodedTTLExceed)
}
