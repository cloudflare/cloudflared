package quic

import (
	"context"
	"net"

	"github.com/quic-go/quic-go/logging"
	"github.com/rs/zerolog"
)

// QUICTracer is a wrapper to create new quicConnTracer
type tracer struct {
	index  string
	logger *zerolog.Logger
}

func NewClientTracer(logger *zerolog.Logger, index uint8) func(context.Context, logging.Perspective, logging.ConnectionID) *logging.ConnectionTracer {
	t := &tracer{
		index:  uint8ToString(index),
		logger: logger,
	}
	return t.TracerForConnection
}

func (t *tracer) TracerForConnection(_ctx context.Context, _p logging.Perspective, _odcid logging.ConnectionID) *logging.ConnectionTracer {
	return newConnTracer(newClientCollector(t.index, t.logger))
}

// connTracer collects connection level metrics
type connTracer struct {
	metricsCollector *clientCollector
}

func newConnTracer(metricsCollector *clientCollector) *logging.ConnectionTracer {
	tracer := connTracer{
		metricsCollector: metricsCollector,
	}
	return &logging.ConnectionTracer{
		StartedConnection:           tracer.StartedConnection,
		ClosedConnection:            tracer.ClosedConnection,
		ReceivedTransportParameters: tracer.ReceivedTransportParameters,
		SentLongHeaderPacket:        tracer.SentLongHeaderPacket,
		SentShortHeaderPacket:       tracer.SentShortHeaderPacket,
		ReceivedLongHeaderPacket:    tracer.ReceivedLongHeaderPacket,
		ReceivedShortHeaderPacket:   tracer.ReceivedShortHeaderPacket,
		BufferedPacket:              tracer.BufferedPacket,
		DroppedPacket:               tracer.DroppedPacket,
		UpdatedMetrics:              tracer.UpdatedMetrics,
		LostPacket:                  tracer.LostPacket,
		UpdatedMTU:                  tracer.UpdatedMTU,
		UpdatedCongestionState:      tracer.UpdatedCongestionState,
	}
}

func (ct *connTracer) StartedConnection(local, remote net.Addr, srcConnID, destConnID logging.ConnectionID) {
	ct.metricsCollector.startedConnection()
}

func (ct *connTracer) ClosedConnection(err error) {
	ct.metricsCollector.closedConnection(err)
}

func (ct *connTracer) ReceivedTransportParameters(params *logging.TransportParameters) {
	ct.metricsCollector.receivedTransportParameters(params)
}

func (ct *connTracer) BufferedPacket(pt logging.PacketType, size logging.ByteCount) {
	ct.metricsCollector.bufferedPackets(pt)
}

func (ct *connTracer) DroppedPacket(pt logging.PacketType, number logging.PacketNumber, size logging.ByteCount, reason logging.PacketDropReason) {
	ct.metricsCollector.droppedPackets(pt, size, reason)
}

func (ct *connTracer) LostPacket(level logging.EncryptionLevel, number logging.PacketNumber, reason logging.PacketLossReason) {
	ct.metricsCollector.lostPackets(reason)
}

func (ct *connTracer) UpdatedMetrics(rttStats *logging.RTTStats, cwnd, bytesInFlight logging.ByteCount, packetsInFlight int) {
	ct.metricsCollector.updatedRTT(rttStats)
	ct.metricsCollector.updateCongestionWindow(cwnd)
}

func (ct *connTracer) SentLongHeaderPacket(hdr *logging.ExtendedHeader, size logging.ByteCount, ecn logging.ECN, ack *logging.AckFrame, frames []logging.Frame) {
	ct.metricsCollector.sentPackets(size, frames)
}

func (ct *connTracer) SentShortHeaderPacket(hdr *logging.ShortHeader, size logging.ByteCount, ecn logging.ECN, ack *logging.AckFrame, frames []logging.Frame) {
	ct.metricsCollector.sentPackets(size, frames)
}

func (ct *connTracer) ReceivedLongHeaderPacket(hdr *logging.ExtendedHeader, size logging.ByteCount, ecn logging.ECN, frames []logging.Frame) {
	ct.metricsCollector.receivedPackets(size, frames)
}

func (ct *connTracer) ReceivedShortHeaderPacket(hdr *logging.ShortHeader, size logging.ByteCount, ecn logging.ECN, frames []logging.Frame) {
	ct.metricsCollector.receivedPackets(size, frames)
}

func (ct *connTracer) UpdatedMTU(mtu logging.ByteCount, done bool) {
	ct.metricsCollector.updateMTU(mtu)
}

func (ct *connTracer) UpdatedCongestionState(state logging.CongestionState) {
	ct.metricsCollector.updatedCongestionState(state)
}
