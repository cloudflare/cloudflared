package quic

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/packet"
	"github.com/cloudflare/cloudflared/tracing"
)

type DatagramV2Type byte

const (
	// UDP payload
	DatagramTypeUDP DatagramV2Type = iota
	// Full IP packet
	DatagramTypeIP
	// DatagramTypeIP + tracing ID
	DatagramTypeIPWithTrace
	// Tracing spans in protobuf format
	DatagramTypeTracingSpan
)

type Packet interface {
	Type() DatagramV2Type
	Payload() []byte
	Metadata() []byte
}

const (
	typeIDLen = 1
	// Same as sessionDemuxChan capacity
	packetChanCapacity = 128
)

func SuffixType(b []byte, datagramType DatagramV2Type) ([]byte, error) {
	if len(b)+typeIDLen > MaxDatagramFrameSize {
		return nil, fmt.Errorf("datagram size %d exceeds max frame size %d", len(b), MaxDatagramFrameSize)
	}
	b = append(b, byte(datagramType))
	return b, nil
}

// Maximum application payload to send to / receive from QUIC datagram frame
func (dm *DatagramMuxerV2) mtu() int {
	return maxDatagramPayloadSize
}

type DatagramMuxerV2 struct {
	session          quic.Connection
	logger           *zerolog.Logger
	sessionDemuxChan chan<- *packet.Session
	packetDemuxChan  chan Packet
}

func NewDatagramMuxerV2(
	quicSession quic.Connection,
	log *zerolog.Logger,
	sessionDemuxChan chan<- *packet.Session,
) *DatagramMuxerV2 {
	logger := log.With().Uint8("datagramVersion", 2).Logger()
	return &DatagramMuxerV2{
		session:          quicSession,
		logger:           &logger,
		sessionDemuxChan: sessionDemuxChan,
		packetDemuxChan:  make(chan Packet, packetChanCapacity),
	}
}

// SendToSession suffix the session ID and datagram version to the payload so the other end of the QUIC connection can
// demultiplex the payload from multiple datagram sessions
func (dm *DatagramMuxerV2) SendToSession(session *packet.Session) error {
	if len(session.Payload) > dm.mtu() {
		packetTooBigDropped.Inc()
		return fmt.Errorf("origin UDP payload has %d bytes, which exceeds transport MTU %d", len(session.Payload), dm.mtu())
	}
	msgWithID, err := SuffixSessionID(session.ID, session.Payload)
	if err != nil {
		return errors.Wrap(err, "Failed to suffix session ID to datagram, it will be dropped")
	}
	msgWithIDAndType, err := SuffixType(msgWithID, DatagramTypeUDP)
	if err != nil {
		return errors.Wrap(err, "Failed to suffix datagram type, it will be dropped")
	}
	if err := dm.session.SendMessage(msgWithIDAndType); err != nil {
		return errors.Wrap(err, "Failed to send datagram back to edge")
	}
	return nil
}

// SendPacket sends a packet with datagram version in the suffix. If ctx is a TracedContext, it adds the tracing
// context between payload and datagram version.
// The other end of the QUIC connection can demultiplex by parsing the payload as IP and look at the source and destination.
func (dm *DatagramMuxerV2) SendPacket(pk Packet) error {
	payloadWithMetadata, err := suffixMetadata(pk.Payload(), pk.Metadata())
	if err != nil {
		return err
	}
	payloadWithMetadataAndType, err := SuffixType(payloadWithMetadata, pk.Type())
	if err != nil {
		return errors.Wrap(err, "Failed to suffix datagram type, it will be dropped")
	}
	if err := dm.session.SendMessage(payloadWithMetadataAndType); err != nil {
		return errors.Wrap(err, "Failed to send datagram back to edge")
	}
	return nil
}

// Demux reads datagrams from the QUIC connection and demuxes depending on whether it's a session or packet
func (dm *DatagramMuxerV2) ServeReceive(ctx context.Context) error {
	for {
		msg, err := dm.session.ReceiveMessage()
		if err != nil {
			return err
		}
		if err := dm.demux(ctx, msg); err != nil {
			dm.logger.Error().Err(err).Msg("Failed to demux datagram")
			if err == context.Canceled {
				return err
			}
		}
	}
}

func (dm *DatagramMuxerV2) ReceivePacket(ctx context.Context) (pk Packet, err error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case pk := <-dm.packetDemuxChan:
		return pk, nil
	}
}

func (dm *DatagramMuxerV2) demux(ctx context.Context, msgWithType []byte) error {
	if len(msgWithType) < typeIDLen {
		return fmt.Errorf("QUIC datagram should have at least %d byte", typeIDLen)
	}
	msgType := DatagramV2Type(msgWithType[len(msgWithType)-typeIDLen])
	msg := msgWithType[0 : len(msgWithType)-typeIDLen]
	switch msgType {
	case DatagramTypeUDP:
		return dm.handleSession(ctx, msg)
	default:
		return dm.handlePacket(ctx, msg, msgType)
	}
}

func (dm *DatagramMuxerV2) handleSession(ctx context.Context, session []byte) error {
	sessionID, payload, err := extractSessionID(session)
	if err != nil {
		return err
	}
	sessionDatagram := packet.Session{
		ID:      sessionID,
		Payload: payload,
	}
	select {
	case dm.sessionDemuxChan <- &sessionDatagram:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (dm *DatagramMuxerV2) handlePacket(ctx context.Context, pk []byte, msgType DatagramV2Type) error {
	var demuxedPacket Packet
	switch msgType {
	case DatagramTypeIP:
		demuxedPacket = RawPacket(packet.RawPacket{Data: pk})
	case DatagramTypeIPWithTrace:
		tracingIdentity, payload, err := extractTracingIdentity(pk)
		if err != nil {
			return err
		}
		demuxedPacket = &TracedPacket{
			Packet:          packet.RawPacket{Data: payload},
			TracingIdentity: tracingIdentity,
		}
	case DatagramTypeTracingSpan:
		tracingIdentity, spans, err := extractTracingIdentity(pk)
		if err != nil {
			return err
		}
		demuxedPacket = &TracingSpanPacket{
			Spans:           spans,
			TracingIdentity: tracingIdentity,
		}
	default:
		return fmt.Errorf("Unexpected datagram type %d", msgType)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case dm.packetDemuxChan <- demuxedPacket:
		return nil
	}
}

func extractTracingIdentity(pk []byte) (tracingIdentity []byte, payload []byte, err error) {
	if len(pk) < tracing.IdentityLength {
		return nil, nil, fmt.Errorf("packet with tracing context should have at least %d bytes, got %v", tracing.IdentityLength, pk)
	}
	tracingIdentity = pk[len(pk)-tracing.IdentityLength:]
	payload = pk[:len(pk)-tracing.IdentityLength]
	return tracingIdentity, payload, nil
}

type RawPacket packet.RawPacket

func (rw RawPacket) Type() DatagramV2Type {
	return DatagramTypeIP
}

func (rw RawPacket) Payload() []byte {
	return rw.Data
}

func (rw RawPacket) Metadata() []byte {
	return []byte{}
}

type TracedPacket struct {
	Packet          packet.RawPacket
	TracingIdentity []byte
}

func (tp *TracedPacket) Type() DatagramV2Type {
	return DatagramTypeIPWithTrace
}

func (tp *TracedPacket) Payload() []byte {
	return tp.Packet.Data
}

func (tp *TracedPacket) Metadata() []byte {
	return tp.TracingIdentity
}

type TracingSpanPacket struct {
	Spans           []byte
	TracingIdentity []byte
}

func (tsp *TracingSpanPacket) Type() DatagramV2Type {
	return DatagramTypeTracingSpan
}

func (tsp *TracingSpanPacket) Payload() []byte {
	return tsp.Spans
}

func (tsp *TracingSpanPacket) Metadata() []byte {
	return tsp.TracingIdentity
}
