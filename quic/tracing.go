package quic

import (
	"context"
	"time"

	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/rs/zerolog"
)

// tracer builds a connTracer for each new QUIC connection.
type tracer struct {
	index  string
	logger *zerolog.Logger
}

func NewClientTracer(logger *zerolog.Logger, index uint8) func(context.Context, bool, qlog.ConnectionID) qlogwriter.Trace {
	t := &tracer{
		index:  uint8ToString(index),
		logger: logger,
	}
	return t.TracerForConnection
}

// TracerForConnection returns a qlogwriter.Trace for a new connection.
func (t *tracer) TracerForConnection(_ context.Context, _ bool, _ qlog.ConnectionID) qlogwriter.Trace {
	return newConnTracer(newClientCollector(t.index, t.logger))
}

// connTracer collects connection level metrics. It implements
// qlogwriter.Trace + qlogwriter.Recorder and dispatches qlog events to the
// metric-collection methods via RecordEvent.
type connTracer struct {
	metricsCollector *clientCollector
}

func newConnTracer(metricsCollector *clientCollector) *connTracer {
	return &connTracer{
		metricsCollector: metricsCollector,
	}
}

func (ct *connTracer) AddProducer() qlogwriter.Recorder {
	// connTracer is both the Trace and the Recorder: each connection gets
	// exactly one producer that routes events to the collector methods below.
	return ct
}

func (ct *connTracer) SupportsSchemas(_ string) bool {
	return true
}

// RecordEvent dispatches qlog events to the collector methods.
func (ct *connTracer) RecordEvent(ev qlogwriter.Event) {
	switch e := ev.(type) {
	case qlog.StartedConnection:
		ct.StartedConnection()
	case qlog.ConnectionClosed:
		ct.ClosedConnection()
	case qlog.ParametersSet:
		// ParametersSet fires for both local and remote; filter to remote only
		// via the Initiator field.
		if e.Initiator == qlog.InitiatorRemote {
			ct.ReceivedTransportParameters(int64(e.MaxUDPPayloadSize), e.MaxIdleTimeout, int64(e.MaxDatagramFrameSize))
		}
	case qlog.PacketSent:
		ct.SentPacket(int64(e.Raw.Length), e.Frames)
	case qlog.PacketReceived:
		ct.ReceivedPacket(int64(e.Raw.Length), e.Frames)
	case qlog.PacketBuffered:
		ct.BufferedPacket(e.Header.PacketType)
	case qlog.PacketDropped:
		ct.DroppedPacket(e.Header.PacketType, int64(e.Raw.Length), e.Trigger)
	case qlog.PacketLost:
		ct.LostPacket(e.Trigger)
	case qlog.MetricsUpdated:
		ct.UpdatedMetrics(e)
	case qlog.MTUUpdated:
		ct.UpdatedMTU(int64(e.Value))
	case qlog.CongestionStateUpdated:
		ct.UpdatedCongestionState(e.State)
	}
}

func (ct *connTracer) Close() error {
	return nil
}

func (ct *connTracer) StartedConnection() {
	ct.metricsCollector.startedConnection()
}

func (ct *connTracer) ClosedConnection() {
	ct.metricsCollector.closedConnection()
}

func (ct *connTracer) ReceivedTransportParameters(maxUDPPayloadSize int64, maxIdleTimeout time.Duration, maxDatagramFrameSize int64) {
	ct.metricsCollector.receivedTransportParameters(maxUDPPayloadSize, maxIdleTimeout, maxDatagramFrameSize)
}

func (ct *connTracer) SentPacket(size int64, frames []qlog.Frame) {
	ct.metricsCollector.sentPackets(size, frames)
}

func (ct *connTracer) ReceivedPacket(size int64, frames []qlog.Frame) {
	ct.metricsCollector.receivedPackets(size, frames)
}

func (ct *connTracer) BufferedPacket(pt qlog.PacketType) {
	ct.metricsCollector.bufferedPackets(pt)
}

func (ct *connTracer) DroppedPacket(pt qlog.PacketType, size int64, reason qlog.PacketDropReason) {
	ct.metricsCollector.droppedPackets(pt, size, reason)
}

func (ct *connTracer) LostPacket(reason qlog.PacketLossReason) {
	ct.metricsCollector.lostPackets(reason)
}

func (ct *connTracer) UpdatedMetrics(m qlog.MetricsUpdated) {
	ct.metricsCollector.updatedRTT(m)
	ct.metricsCollector.updateCongestionWindow(int64(m.CongestionWindow))
}

func (ct *connTracer) UpdatedMTU(mtu int64) {
	ct.metricsCollector.updateMTU(mtu)
}

func (ct *connTracer) UpdatedCongestionState(state qlog.CongestionState) {
	ct.metricsCollector.updatedCongestionState(state)
}
