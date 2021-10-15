package quic

import (
	"context"
	"net"

	"github.com/lucas-clemente/quic-go/logging"
	"github.com/lucas-clemente/quic-go/qlog"
	"github.com/rs/zerolog"
)

// QUICTracer is a wrapper to create new quicConnTracer
type tracer struct {
	logger *zerolog.Logger
	config *tracerConfig
}

type tracerConfig struct {
	isClient bool
	// Only client has an index
	index uint8
}

func NewClientTracer(logger *zerolog.Logger, index uint8) logging.Tracer {
	return &tracer{
		logger: logger,
		config: &tracerConfig{
			isClient: true,
			index:    index,
		},
	}
}

func NewServerTracer(logger *zerolog.Logger) logging.Tracer {
	return &tracer{
		logger: logger,
		config: &tracerConfig{
			isClient: false,
		},
	}
}

func (t *tracer) TracerForConnection(_ context.Context, p logging.Perspective, odcid logging.ConnectionID) logging.ConnectionTracer {
	connID := logging.ConnectionID(odcid).String()
	ql := &quicLogger{
		logger:       t.logger,
		connectionID: connID,
	}
	if t.config.isClient {
		return newConnTracer(ql, p, odcid, newClientCollector(t.config.index))
	}
	return newConnTracer(ql, p, odcid, newServiceCollector())
}

func (*tracer) SentPacket(net.Addr, *logging.Header, logging.ByteCount, []logging.Frame) {}
func (*tracer) DroppedPacket(net.Addr, logging.PacketType, logging.ByteCount, logging.PacketDropReason) {
}

// connTracer is a wrapper around https://pkg.go.dev/github.com/lucas-clemente/quic-go@v0.23.0/qlog#NewConnectionTracer to collect metrics
type connTracer struct {
	logging.ConnectionTracer
	metricsCollector MetricsCollector
	connectionID     string
}

func newConnTracer(ql *quicLogger, p logging.Perspective, odcid logging.ConnectionID, metricsCollector MetricsCollector) logging.ConnectionTracer {
	return &connTracer{
		qlog.NewConnectionTracer(ql, p, odcid),
		metricsCollector,
		logging.ConnectionID(odcid).String(),
	}
}

func (ct *connTracer) StartedConnection(local, remote net.Addr, srcConnID, destConnID logging.ConnectionID) {
	ct.metricsCollector.startedConnection()
	ct.ConnectionTracer.StartedConnection(local, remote, srcConnID, destConnID)
}

func (ct *connTracer) ClosedConnection(err error) {
	ct.metricsCollector.closedConnection(err)
	ct.ConnectionTracer.ClosedConnection(err)
}

func (ct *connTracer) SentPacket(hdr *logging.ExtendedHeader, packetSize logging.ByteCount, ack *logging.AckFrame, frames []logging.Frame) {
	ct.metricsCollector.sentPackets(packetSize)
	ct.ConnectionTracer.SentPacket(hdr, packetSize, ack, frames)
}

func (ct *connTracer) ReceivedPacket(hdr *logging.ExtendedHeader, size logging.ByteCount, frames []logging.Frame) {
	ct.metricsCollector.receivedPackets(size)
	ct.ConnectionTracer.ReceivedPacket(hdr, size, frames)
}

func (ct *connTracer) BufferedPacket(pt logging.PacketType) {
	ct.metricsCollector.bufferedPackets(pt)
	ct.ConnectionTracer.BufferedPacket(pt)
}

func (ct *connTracer) DroppedPacket(pt logging.PacketType, size logging.ByteCount, reason logging.PacketDropReason) {
	ct.metricsCollector.droppedPackets(pt, size, reason)
	ct.ConnectionTracer.DroppedPacket(pt, size, reason)
}

func (ct *connTracer) LostPacket(level logging.EncryptionLevel, number logging.PacketNumber, reason logging.PacketLossReason) {
	ct.metricsCollector.lostPackets(reason)
	ct.ConnectionTracer.LostPacket(level, number, reason)
}

func (ct *connTracer) UpdatedMetrics(rttStats *logging.RTTStats, cwnd, bytesInFlight logging.ByteCount, packetsInFlight int) {
	ct.metricsCollector.updatedRTT(rttStats)
	ct.ConnectionTracer.UpdatedMetrics(rttStats, cwnd, bytesInFlight, packetsInFlight)
}

type quicLogger struct {
	logger       *zerolog.Logger
	connectionID string
}

func (qt *quicLogger) Write(p []byte) (n int, err error) {
	qt.logger.Trace().Str("quicConnection", qt.connectionID).RawJSON("event", p).Msg("Quic event")
	return len(p), nil
}

func (*quicLogger) Close() error {
	return nil
}
