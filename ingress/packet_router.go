package ingress

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudflare/cloudflared/packet"
	quicpogs "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/tracing"
)

// Upstream of raw packets
type muxer interface {
	SendPacket(pk quicpogs.Packet) error
	// ReceivePacket waits for the next raw packet from upstream
	ReceivePacket(ctx context.Context) (quicpogs.Packet, error)
}

// PacketRouter routes packets between Upstream and ICMPRouter. Currently it rejects all other type of ICMP packets
type PacketRouter struct {
	icmpRouter ICMPRouter
	muxer      muxer
	connIndex  uint8
	logger     *zerolog.Logger
	encoder    *packet.Encoder
	decoder    *packet.ICMPDecoder
}

// NewPacketRouter creates a PacketRouter that handles ICMP packets. Packets are read from muxer but dropped if globalConfig is nil.
func NewPacketRouter(icmpRouter ICMPRouter, muxer muxer, connIndex uint8, logger *zerolog.Logger) *PacketRouter {
	return &PacketRouter{
		icmpRouter: icmpRouter,
		muxer:      muxer,
		connIndex:  connIndex,
		logger:     logger,
		encoder:    packet.NewEncoder(),
		decoder:    packet.NewICMPDecoder(),
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

func (r *PacketRouter) nextPacket(ctx context.Context) (packet.RawPacket, ICMPResponder, error) {
	pk, err := r.muxer.ReceivePacket(ctx)
	if err != nil {
		return packet.RawPacket{}, nil, err
	}
	responder := newPacketResponder(r.muxer, r.connIndex, packet.NewEncoder())

	switch pk.Type() {
	case quicpogs.DatagramTypeIP:
		return packet.RawPacket{Data: pk.Payload()}, responder, nil
	case quicpogs.DatagramTypeIPWithTrace:
		var identity tracing.Identity
		if err := identity.UnmarshalBinary(pk.Metadata()); err != nil {
			r.logger.Err(err).Bytes("tracingIdentity", pk.Metadata()).Msg("Failed to unmarshal tracing identity")
		} else {
			tracedCtx := tracing.NewTracedContext(ctx, identity.String(), r.logger)
			responder.AddTraceContext(tracedCtx, pk.Metadata())
		}
		return packet.RawPacket{Data: pk.Payload()}, responder, nil
	default:
		return packet.RawPacket{}, nil, fmt.Errorf("unexpected datagram type %d", pk.Type())
	}
}

func (r *PacketRouter) handlePacket(ctx context.Context, rawPacket packet.RawPacket, responder ICMPResponder) {
	// ICMP Proxy feature is disabled, drop packets
	if r.icmpRouter == nil {
		return
	}

	icmpPacket, err := r.decoder.Decode(rawPacket)
	if err != nil {
		r.logger.Err(err).Msg("Failed to decode ICMP packet from quic datagram")
		return
	}

	if icmpPacket.TTL <= 1 {
		if err := r.sendTTLExceedMsg(icmpPacket, rawPacket); err != nil {
			r.logger.Err(err).Msg("Failed to return ICMP TTL exceed error")
		}
		return
	}
	icmpPacket.TTL--

	if err := r.icmpRouter.Request(ctx, icmpPacket, responder); err != nil {
		r.logger.Err(err).
			Str("src", icmpPacket.Src.String()).
			Str("dst", icmpPacket.Dst.String()).
			Interface("type", icmpPacket.Type).
			Msg("Failed to send ICMP packet")
	}
}

func (r *PacketRouter) sendTTLExceedMsg(pk *packet.ICMP, rawPacket packet.RawPacket) error {
	icmpTTLPacket := r.icmpRouter.ConvertToTTLExceeded(pk, rawPacket)
	encodedTTLExceed, err := r.encoder.Encode(icmpTTLPacket)
	if err != nil {
		return err
	}
	return r.muxer.SendPacket(quicpogs.RawPacket(encodedTTLExceed))
}

// packetResponder should not be used concurrently. This assumption is upheld because reply packets are ready one-by-one
type packetResponder struct {
	datagramMuxer      muxer
	connIndex          uint8
	encoder            *packet.Encoder
	tracedCtx          *tracing.TracedContext
	serializedIdentity []byte
	// hadReply tracks if there has been any reply for this flow
	hadReply bool
}

func newPacketResponder(datagramMuxer muxer, connIndex uint8, encoder *packet.Encoder) ICMPResponder {
	return &packetResponder{
		datagramMuxer: datagramMuxer,
		connIndex:     connIndex,
		encoder:       encoder,
	}
}

func (pr *packetResponder) tracingEnabled() bool {
	return pr.tracedCtx != nil
}

func (pr *packetResponder) ConnectionIndex() uint8 {
	return pr.connIndex
}

func (pr *packetResponder) ReturnPacket(pk *packet.ICMP) error {
	rawPacket, err := pr.encoder.Encode(pk)
	if err != nil {
		return err
	}
	pr.hadReply = true
	return pr.datagramMuxer.SendPacket(quicpogs.RawPacket(rawPacket))
}

func (pr *packetResponder) AddTraceContext(tracedCtx *tracing.TracedContext, serializedIdentity []byte) {
	pr.tracedCtx = tracedCtx
	pr.serializedIdentity = serializedIdentity
}

func (pr *packetResponder) RequestSpan(ctx context.Context, pk *packet.ICMP) (context.Context, trace.Span) {
	if !pr.tracingEnabled() {
		return ctx, tracing.NewNoopSpan()
	}
	return pr.tracedCtx.Tracer().Start(pr.tracedCtx, "icmp-echo-request", trace.WithAttributes(
		attribute.String("src", pk.Src.String()),
		attribute.String("dst", pk.Dst.String()),
	))
}

func (pr *packetResponder) ReplySpan(ctx context.Context, logger *zerolog.Logger) (context.Context, trace.Span) {
	if !pr.tracingEnabled() || pr.hadReply {
		return ctx, tracing.NewNoopSpan()
	}
	return pr.tracedCtx.Tracer().Start(pr.tracedCtx, "icmp-echo-reply")
}

func (pr *packetResponder) ExportSpan() {
	if !pr.tracingEnabled() {
		return
	}
	spans := pr.tracedCtx.GetProtoSpans()
	if len(spans) > 0 {
		pr.datagramMuxer.SendPacket(&quicpogs.TracingSpanPacket{
			Spans:           spans,
			TracingIdentity: pr.serializedIdentity,
		})
	}
}
