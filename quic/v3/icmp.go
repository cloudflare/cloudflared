package v3

import (
	"context"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"

	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/packet"
	"github.com/cloudflare/cloudflared/tracing"
)

// packetResponder is an implementation of the [ingress.ICMPResponder] which provides the ICMP Flow manager the
// return path to return and ICMP Echo response back to the QUIC muxer.
type packetResponder struct {
	datagramMuxer DatagramICMPWriter
	connID        uint8
}

func newPacketResponder(datagramMuxer DatagramICMPWriter, connID uint8) ingress.ICMPResponder {
	return &packetResponder{
		datagramMuxer,
		connID,
	}
}

func (pr *packetResponder) ConnectionIndex() uint8 {
	return pr.connID
}

func (pr *packetResponder) ReturnPacket(pk *packet.ICMP) error {
	return pr.datagramMuxer.SendICMPPacket(pk)
}

func (pr *packetResponder) AddTraceContext(tracedCtx *tracing.TracedContext, serializedIdentity []byte) {
	// datagram v3 does not support tracing ICMP packets
}

func (pr *packetResponder) RequestSpan(ctx context.Context, pk *packet.ICMP) (context.Context, trace.Span) {
	// datagram v3 does not support tracing ICMP packets
	return ctx, tracing.NewNoopSpan()
}

func (pr *packetResponder) ReplySpan(ctx context.Context, logger *zerolog.Logger) (context.Context, trace.Span) {
	// datagram v3 does not support tracing ICMP packets
	return ctx, tracing.NewNoopSpan()
}

func (pr *packetResponder) ExportSpan() {
	// datagram v3 does not support tracing ICMP packets
}
