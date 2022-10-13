package ingress

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/packet"
	quicpogs "github.com/cloudflare/cloudflared/quic"
)

// Upstream of raw packets
type muxer interface {
	SendPacket(pk quicpogs.Packet) error
	// ReceivePacket waits for the next raw packet from upstream
	ReceivePacket(ctx context.Context) (quicpogs.Packet, error)
}

// PacketRouter routes packets between Upstream and ICMPRouter. Currently it rejects all other type of ICMP packets
type PacketRouter struct {
	globalConfig           *GlobalRouterConfig
	muxer                  muxer
	logger                 *zerolog.Logger
	checkRouterEnabledFunc func() bool
	icmpDecoder            *packet.ICMPDecoder
	encoder                *packet.Encoder
}

// GlobalRouterConfig is the configuration shared by all instance of Router.
type GlobalRouterConfig struct {
	ICMPRouter *icmpRouter
	IPv4Src    netip.Addr
	IPv6Src    netip.Addr
	Zone       string
}

// NewPacketRouter creates a PacketRouter that handles ICMP packets. Packets are read from muxer but dropped if globalConfig is nil.
func NewPacketRouter(globalConfig *GlobalRouterConfig, muxer muxer, logger *zerolog.Logger, checkRouterEnabledFunc func() bool) *PacketRouter {
	return &PacketRouter{
		globalConfig:           globalConfig,
		muxer:                  muxer,
		logger:                 logger,
		checkRouterEnabledFunc: checkRouterEnabledFunc,
		icmpDecoder:            packet.NewICMPDecoder(),
		encoder:                packet.NewEncoder(),
	}
}

func (r *PacketRouter) Serve(ctx context.Context) error {
	for {
		rawPacket, responder, err := r.nextPacket(ctx)
		if err != nil {
			return err
		}
		r.handlePacket(ctx, rawPacket, responder)
	}
}

func (r *PacketRouter) nextPacket(ctx context.Context) (packet.RawPacket, *packetResponder, error) {
	pk, err := r.muxer.ReceivePacket(ctx)
	if err != nil {
		return packet.RawPacket{}, nil, err
	}
	responder := &packetResponder{
		datagramMuxer: r.muxer,
	}
	switch pk.Type() {
	case quicpogs.DatagramTypeIP:
		return packet.RawPacket{Data: pk.Payload()}, responder, nil
	case quicpogs.DatagramTypeIPWithTrace:
		return packet.RawPacket{}, responder, fmt.Errorf("TODO: TUN-6604 Handle IP packet with trace")
	default:
		return packet.RawPacket{}, nil, fmt.Errorf("unexpected datagram type %d", pk.Type())
	}
}

func (r *PacketRouter) handlePacket(ctx context.Context, rawPacket packet.RawPacket, responder *packetResponder) {
	// ICMP Proxy feature is disabled, drop packets
	if r.globalConfig == nil {
		return
	}

	if enabled := r.checkRouterEnabledFunc(); !enabled {
		return
	}

	icmpPacket, err := r.icmpDecoder.Decode(rawPacket)
	if err != nil {
		r.logger.Err(err).Msg("Failed to decode ICMP packet from quic datagram")
		return
	}

	if icmpPacket.TTL <= 1 {
		if err := r.sendTTLExceedMsg(ctx, icmpPacket, rawPacket, r.encoder); err != nil {
			r.logger.Err(err).Msg("Failed to return ICMP TTL exceed error")
		}
		return
	}
	icmpPacket.TTL--

	if err := r.globalConfig.ICMPRouter.Request(ctx, icmpPacket, responder); err != nil {
		r.logger.Err(err).
			Str("src", icmpPacket.Src.String()).
			Str("dst", icmpPacket.Dst.String()).
			Interface("type", icmpPacket.Type).
			Msg("Failed to send ICMP packet")
	}
}

func (r *PacketRouter) sendTTLExceedMsg(ctx context.Context, pk *packet.ICMP, rawPacket packet.RawPacket, encoder *packet.Encoder) error {
	var srcIP netip.Addr
	if pk.Dst.Is4() {
		srcIP = r.globalConfig.IPv4Src
	} else {
		srcIP = r.globalConfig.IPv6Src
	}
	ttlExceedPacket := packet.NewICMPTTLExceedPacket(pk.IP, rawPacket, srcIP)

	encodedTTLExceed, err := encoder.Encode(ttlExceedPacket)
	if err != nil {
		return err
	}
	return r.muxer.SendPacket(quicpogs.RawPacket(encodedTTLExceed))
}

type packetResponder struct {
	datagramMuxer muxer
}

func (pr *packetResponder) returnPacket(rawPacket packet.RawPacket) error {
	return pr.datagramMuxer.SendPacket(quicpogs.RawPacket(rawPacket))
}
