package quic

import (
	"context"
	"net"
	"time"

	"github.com/quic-go/quic-go/logging"
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

func NewClientTracer(logger *zerolog.Logger, index uint8) func(context.Context, logging.Perspective, logging.ConnectionID) *logging.ConnectionTracer {
	t := &tracer{
		logger: logger,
		config: &tracerConfig{
			isClient: true,
			index:    index,
		},
	}
	return t.TracerForConnection
}

func NewServerTracer(logger *zerolog.Logger) *logging.Tracer {
	return &logging.Tracer{
		SentPacket:                   func(net.Addr, *logging.Header, logging.ByteCount, []logging.Frame) {},
		SentVersionNegotiationPacket: func(_ net.Addr, dest, src logging.ArbitraryLenConnectionID, _ []logging.VersionNumber) {},
		DroppedPacket:                func(net.Addr, logging.PacketType, logging.ByteCount, logging.PacketDropReason) {},
	}
}

func (t *tracer) TracerForConnection(_ctx context.Context, _p logging.Perspective, _odcid logging.ConnectionID) *logging.ConnectionTracer {
	if t.config.isClient {
		return newConnTracer(newClientCollector(t.config.index))
	}
	return newConnTracer(newServiceCollector())
}

// connTracer collects connection level metrics
type connTracer struct {
	metricsCollector MetricsCollector
}

func newConnTracer(metricsCollector MetricsCollector) *logging.ConnectionTracer {
	tracer := connTracer{
		metricsCollector: metricsCollector,
	}
	return &logging.ConnectionTracer{
		StartedConnection:                tracer.StartedConnection,
		NegotiatedVersion:                tracer.NegotiatedVersion,
		ClosedConnection:                 tracer.ClosedConnection,
		SentTransportParameters:          tracer.SentTransportParameters,
		ReceivedTransportParameters:      tracer.ReceivedTransportParameters,
		RestoredTransportParameters:      tracer.RestoredTransportParameters,
		SentLongHeaderPacket:             tracer.SentLongHeaderPacket,
		SentShortHeaderPacket:            tracer.SentShortHeaderPacket,
		ReceivedVersionNegotiationPacket: tracer.ReceivedVersionNegotiationPacket,
		ReceivedRetry:                    tracer.ReceivedRetry,
		ReceivedLongHeaderPacket:         tracer.ReceivedLongHeaderPacket,
		ReceivedShortHeaderPacket:        tracer.ReceivedShortHeaderPacket,
		BufferedPacket:                   tracer.BufferedPacket,
		DroppedPacket:                    tracer.DroppedPacket,
		UpdatedMetrics:                   tracer.UpdatedMetrics,
		AcknowledgedPacket:               tracer.AcknowledgedPacket,
		LostPacket:                       tracer.LostPacket,
		UpdatedCongestionState:           tracer.UpdatedCongestionState,
		UpdatedPTOCount:                  tracer.UpdatedPTOCount,
		UpdatedKeyFromTLS:                tracer.UpdatedKeyFromTLS,
		UpdatedKey:                       tracer.UpdatedKey,
		DroppedEncryptionLevel:           tracer.DroppedEncryptionLevel,
		DroppedKey:                       tracer.DroppedKey,
		SetLossTimer:                     tracer.SetLossTimer,
		LossTimerExpired:                 tracer.LossTimerExpired,
		LossTimerCanceled:                tracer.LossTimerCanceled,
		ECNStateUpdated:                  tracer.ECNStateUpdated,
		Close:                            tracer.Close,
		Debug:                            tracer.Debug,
	}
}

func (ct *connTracer) StartedConnection(local, remote net.Addr, srcConnID, destConnID logging.ConnectionID) {
	ct.metricsCollector.startedConnection()
}

func (ct *connTracer) ClosedConnection(err error) {
	ct.metricsCollector.closedConnection(err)
}

func (ct *connTracer) SentPacket(hdr *logging.ExtendedHeader, packetSize logging.ByteCount, ack *logging.AckFrame, frames []logging.Frame) {
	ct.metricsCollector.sentPackets(packetSize)
}

func (ct *connTracer) ReceivedPacket(hdr *logging.ExtendedHeader, size logging.ByteCount, frames []logging.Frame) {
	ct.metricsCollector.receivedPackets(size)
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
}

func (ct *connTracer) NegotiatedVersion(chosen logging.VersionNumber, clientVersions, serverVersions []logging.VersionNumber) {
}

func (ct *connTracer) SentTransportParameters(parameters *logging.TransportParameters) {
}

func (ct *connTracer) ReceivedTransportParameters(parameters *logging.TransportParameters) {
}

func (ct *connTracer) RestoredTransportParameters(parameters *logging.TransportParameters) {
}

func (ct *connTracer) SentLongHeaderPacket(hdr *logging.ExtendedHeader, size logging.ByteCount, ecn logging.ECN, ack *logging.AckFrame, frames []logging.Frame) {
}

func (ct *connTracer) SentShortHeaderPacket(hdr *logging.ShortHeader, size logging.ByteCount, ecn logging.ECN, ack *logging.AckFrame, frames []logging.Frame) {
}

func (ct *connTracer) ReceivedVersionNegotiationPacket(dest, src logging.ArbitraryLenConnectionID, _ []logging.VersionNumber) {
}

func (ct *connTracer) ReceivedRetry(header *logging.Header) {
}

func (ct *connTracer) ReceivedLongHeaderPacket(hdr *logging.ExtendedHeader, size logging.ByteCount, ecn logging.ECN, frames []logging.Frame) {
}

func (ct *connTracer) ReceivedShortHeaderPacket(hdr *logging.ShortHeader, size logging.ByteCount, ecn logging.ECN, frames []logging.Frame) {
}

func (ct *connTracer) AcknowledgedPacket(level logging.EncryptionLevel, number logging.PacketNumber) {
}

func (ct *connTracer) UpdatedCongestionState(state logging.CongestionState) {
}

func (ct *connTracer) UpdatedPTOCount(value uint32) {
}

func (ct *connTracer) UpdatedKeyFromTLS(level logging.EncryptionLevel, perspective logging.Perspective) {
}

func (ct *connTracer) UpdatedKey(generation logging.KeyPhase, remote bool) {
}

func (ct *connTracer) DroppedEncryptionLevel(level logging.EncryptionLevel) {
}

func (ct *connTracer) DroppedKey(generation logging.KeyPhase) {
}

func (ct *connTracer) SetLossTimer(timerType logging.TimerType, level logging.EncryptionLevel, time time.Time) {
}

func (ct *connTracer) LossTimerExpired(timerType logging.TimerType, level logging.EncryptionLevel) {
}

func (ct *connTracer) LossTimerCanceled() {
}

func (ct *connTracer) ECNStateUpdated(state logging.ECNState, trigger logging.ECNStateTrigger) {

}

func (ct *connTracer) Close() {
}

func (ct *connTracer) Debug(name, msg string) {
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
