package ackhandler

import (
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// SentPacketHandler handles ACKs received for outgoing packets
type SentPacketHandler interface {
	// SentPacket may modify the packet
	SentPacket(t time.Time, pn, largestAcked protocol.PacketNumber, streamFrames []StreamFrame, frames []Frame, encLevel protocol.EncryptionLevel, ecn protocol.ECN, size protocol.ByteCount, isPathMTUProbePacket, isPathProbePacket bool)
	// ReceivedAck processes an ACK frame.
	// It does not store a copy of the frame.
	ReceivedAck(f *wire.AckFrame, encLevel protocol.EncryptionLevel, rcvTime time.Time) (bool /* 1-RTT packet acked */, error)
	ReceivedBytes(_ protocol.ByteCount, rcvTime time.Time)
	DropPackets(_ protocol.EncryptionLevel, rcvTime time.Time)
	ResetForRetry(rcvTime time.Time)

	// The SendMode determines if and what kind of packets can be sent.
	SendMode(now time.Time) SendMode
	// TimeUntilSend is the time when the next packet should be sent.
	// It is used for pacing packets.
	TimeUntilSend() time.Time
	SetMaxDatagramSize(count protocol.ByteCount)

	// only to be called once the handshake is complete
	QueueProbePacket(protocol.EncryptionLevel) bool /* was a packet queued */

	ECNMode(isShortHeaderPacket bool) protocol.ECN // isShortHeaderPacket should only be true for non-coalesced 1-RTT packets
	PeekPacketNumber(protocol.EncryptionLevel) (protocol.PacketNumber, protocol.PacketNumberLen)
	PopPacketNumber(protocol.EncryptionLevel) protocol.PacketNumber

	GetLossDetectionTimeout() time.Time
	OnLossDetectionTimeout(now time.Time) error

	MigratedPath(now time.Time, initialMaxPacketSize protocol.ByteCount)
}

type sentPacketTracker interface {
	GetLowestPacketNotConfirmedAcked() protocol.PacketNumber
	ReceivedPacket(_ protocol.EncryptionLevel, rcvTime time.Time)
}

// ReceivedPacketHandler handles ACKs needed to send for incoming packets
type ReceivedPacketHandler interface {
	IsPotentiallyDuplicate(protocol.PacketNumber, protocol.EncryptionLevel) bool
	ReceivedPacket(pn protocol.PacketNumber, ecn protocol.ECN, encLevel protocol.EncryptionLevel, rcvTime time.Time, ackEliciting bool) error
	DropPackets(protocol.EncryptionLevel)

	GetAlarmTimeout() time.Time
	GetAckFrame(_ protocol.EncryptionLevel, now time.Time, onlyIfQueued bool) *wire.AckFrame
}
