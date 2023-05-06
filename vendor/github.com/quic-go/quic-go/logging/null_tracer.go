package logging

import (
	"net"
	"time"
)

// The NullTracer is a Tracer that does nothing.
// It is useful for embedding.
type NullTracer struct{}

var _ Tracer = &NullTracer{}

func (n NullTracer) SentPacket(net.Addr, *Header, ByteCount, []Frame) {}
func (n NullTracer) SentVersionNegotiationPacket(_ net.Addr, dest, src ArbitraryLenConnectionID, _ []VersionNumber) {
}
func (n NullTracer) DroppedPacket(net.Addr, PacketType, ByteCount, PacketDropReason) {}

// The NullConnectionTracer is a ConnectionTracer that does nothing.
// It is useful for embedding.
type NullConnectionTracer struct{}

var _ ConnectionTracer = &NullConnectionTracer{}

func (n NullConnectionTracer) StartedConnection(local, remote net.Addr, srcConnID, destConnID ConnectionID) {
}

func (n NullConnectionTracer) NegotiatedVersion(chosen VersionNumber, clientVersions, serverVersions []VersionNumber) {
}
func (n NullConnectionTracer) ClosedConnection(err error)                                          {}
func (n NullConnectionTracer) SentTransportParameters(*TransportParameters)                        {}
func (n NullConnectionTracer) ReceivedTransportParameters(*TransportParameters)                    {}
func (n NullConnectionTracer) RestoredTransportParameters(*TransportParameters)                    {}
func (n NullConnectionTracer) SentLongHeaderPacket(*ExtendedHeader, ByteCount, *AckFrame, []Frame) {}
func (n NullConnectionTracer) SentShortHeaderPacket(*ShortHeader, ByteCount, *AckFrame, []Frame)   {}
func (n NullConnectionTracer) ReceivedVersionNegotiationPacket(dest, src ArbitraryLenConnectionID, _ []VersionNumber) {
}
func (n NullConnectionTracer) ReceivedRetry(*Header)                                        {}
func (n NullConnectionTracer) ReceivedLongHeaderPacket(*ExtendedHeader, ByteCount, []Frame) {}
func (n NullConnectionTracer) ReceivedShortHeaderPacket(*ShortHeader, ByteCount, []Frame)   {}
func (n NullConnectionTracer) BufferedPacket(PacketType, ByteCount)                         {}
func (n NullConnectionTracer) DroppedPacket(PacketType, ByteCount, PacketDropReason)        {}

func (n NullConnectionTracer) UpdatedMetrics(rttStats *RTTStats, cwnd, bytesInFlight ByteCount, packetsInFlight int) {
}
func (n NullConnectionTracer) AcknowledgedPacket(EncryptionLevel, PacketNumber)            {}
func (n NullConnectionTracer) LostPacket(EncryptionLevel, PacketNumber, PacketLossReason)  {}
func (n NullConnectionTracer) UpdatedCongestionState(CongestionState)                      {}
func (n NullConnectionTracer) UpdatedPTOCount(uint32)                                      {}
func (n NullConnectionTracer) UpdatedKeyFromTLS(EncryptionLevel, Perspective)              {}
func (n NullConnectionTracer) UpdatedKey(keyPhase KeyPhase, remote bool)                   {}
func (n NullConnectionTracer) DroppedEncryptionLevel(EncryptionLevel)                      {}
func (n NullConnectionTracer) DroppedKey(KeyPhase)                                         {}
func (n NullConnectionTracer) SetLossTimer(TimerType, EncryptionLevel, time.Time)          {}
func (n NullConnectionTracer) LossTimerExpired(timerType TimerType, level EncryptionLevel) {}
func (n NullConnectionTracer) LossTimerCanceled()                                          {}
func (n NullConnectionTracer) Close()                                                      {}
func (n NullConnectionTracer) Debug(name, msg string)                                      {}
