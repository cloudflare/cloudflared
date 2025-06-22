package quic

import (
	"slices"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/logging"
)

// ConvertFrame converts a wire.Frame into a logging.Frame.
// This makes it possible for external packages to access the frames.
// Furthermore, it removes the data slices from CRYPTO and STREAM frames.
func toLoggingFrame(frame wire.Frame) logging.Frame {
	switch f := frame.(type) {
	case *wire.AckFrame:
		// We use a pool for ACK frames.
		// Implementations of the tracer interface may hold on to frames, so we need to make a copy here.
		return toLoggingAckFrame(f)
	case *wire.CryptoFrame:
		return &logging.CryptoFrame{
			Offset: f.Offset,
			Length: protocol.ByteCount(len(f.Data)),
		}
	case *wire.StreamFrame:
		return &logging.StreamFrame{
			StreamID: f.StreamID,
			Offset:   f.Offset,
			Length:   f.DataLen(),
			Fin:      f.Fin,
		}
	case *wire.DatagramFrame:
		return &logging.DatagramFrame{
			Length: logging.ByteCount(len(f.Data)),
		}
	default:
		return logging.Frame(frame)
	}
}

func toLoggingAckFrame(f *wire.AckFrame) *logging.AckFrame {
	ack := &logging.AckFrame{
		AckRanges: slices.Clone(f.AckRanges),
		DelayTime: f.DelayTime,
		ECNCE:     f.ECNCE,
		ECT0:      f.ECT0,
		ECT1:      f.ECT1,
	}
	return ack
}

func (s *connection) logLongHeaderPacket(p *longHeaderPacket, ecn protocol.ECN) {
	// quic-go logging
	if s.logger.Debug() {
		p.header.Log(s.logger)
		if p.ack != nil {
			wire.LogFrame(s.logger, p.ack, true)
		}
		for _, frame := range p.frames {
			wire.LogFrame(s.logger, frame.Frame, true)
		}
		for _, frame := range p.streamFrames {
			wire.LogFrame(s.logger, frame.Frame, true)
		}
	}

	// tracing
	if s.tracer != nil && s.tracer.SentLongHeaderPacket != nil {
		frames := make([]logging.Frame, 0, len(p.frames))
		for _, f := range p.frames {
			frames = append(frames, toLoggingFrame(f.Frame))
		}
		for _, f := range p.streamFrames {
			frames = append(frames, toLoggingFrame(f.Frame))
		}
		var ack *logging.AckFrame
		if p.ack != nil {
			ack = toLoggingAckFrame(p.ack)
		}
		s.tracer.SentLongHeaderPacket(p.header, p.length, ecn, ack, frames)
	}
}

func (s *connection) logShortHeaderPacket(
	destConnID protocol.ConnectionID,
	ackFrame *wire.AckFrame,
	frames []ackhandler.Frame,
	streamFrames []ackhandler.StreamFrame,
	pn protocol.PacketNumber,
	pnLen protocol.PacketNumberLen,
	kp protocol.KeyPhaseBit,
	ecn protocol.ECN,
	size protocol.ByteCount,
	isCoalesced bool,
) {
	if s.logger.Debug() && !isCoalesced {
		s.logger.Debugf("-> Sending packet %d (%d bytes) for connection %s, 1-RTT (ECN: %s)", pn, size, s.logID, ecn)
	}
	// quic-go logging
	if s.logger.Debug() {
		wire.LogShortHeader(s.logger, destConnID, pn, pnLen, kp)
		if ackFrame != nil {
			wire.LogFrame(s.logger, ackFrame, true)
		}
		for _, f := range frames {
			wire.LogFrame(s.logger, f.Frame, true)
		}
		for _, f := range streamFrames {
			wire.LogFrame(s.logger, f.Frame, true)
		}
	}

	// tracing
	if s.tracer != nil && s.tracer.SentShortHeaderPacket != nil {
		fs := make([]logging.Frame, 0, len(frames)+len(streamFrames))
		for _, f := range frames {
			fs = append(fs, toLoggingFrame(f.Frame))
		}
		for _, f := range streamFrames {
			fs = append(fs, toLoggingFrame(f.Frame))
		}
		var ack *logging.AckFrame
		if ackFrame != nil {
			ack = toLoggingAckFrame(ackFrame)
		}
		s.tracer.SentShortHeaderPacket(
			&logging.ShortHeader{DestConnectionID: destConnID, PacketNumber: pn, PacketNumberLen: pnLen, KeyPhase: kp},
			size,
			ecn,
			ack,
			fs,
		)
	}
}

func (s *connection) logCoalescedPacket(packet *coalescedPacket, ecn protocol.ECN) {
	if s.logger.Debug() {
		// There's a short period between dropping both Initial and Handshake keys and completion of the handshake,
		// during which we might call PackCoalescedPacket but just pack a short header packet.
		if len(packet.longHdrPackets) == 0 && packet.shortHdrPacket != nil {
			s.logShortHeaderPacket(
				packet.shortHdrPacket.DestConnID,
				packet.shortHdrPacket.Ack,
				packet.shortHdrPacket.Frames,
				packet.shortHdrPacket.StreamFrames,
				packet.shortHdrPacket.PacketNumber,
				packet.shortHdrPacket.PacketNumberLen,
				packet.shortHdrPacket.KeyPhase,
				ecn,
				packet.shortHdrPacket.Length,
				false,
			)
			return
		}
		if len(packet.longHdrPackets) > 1 {
			s.logger.Debugf("-> Sending coalesced packet (%d parts, %d bytes) for connection %s", len(packet.longHdrPackets), packet.buffer.Len(), s.logID)
		} else {
			s.logger.Debugf("-> Sending packet %d (%d bytes) for connection %s, %s", packet.longHdrPackets[0].header.PacketNumber, packet.buffer.Len(), s.logID, packet.longHdrPackets[0].EncryptionLevel())
		}
	}
	for _, p := range packet.longHdrPackets {
		s.logLongHeaderPacket(p, ecn)
	}
	if p := packet.shortHdrPacket; p != nil {
		s.logShortHeaderPacket(p.DestConnID, p.Ack, p.Frames, p.StreamFrames, p.PacketNumber, p.PacketNumberLen, p.KeyPhase, ecn, p.Length, true)
	}
}
