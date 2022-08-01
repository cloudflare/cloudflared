package quic

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

type datagramV2Type byte

const (
	udp datagramV2Type = iota
	ip
)

func suffixType(b []byte, datagramType datagramV2Type) ([]byte, error) {
	if len(b)+1 > MaxDatagramFrameSize {
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
	sessionDemuxChan chan<- *SessionDatagram
	packetDemuxChan  chan<- []byte
}

func NewDatagramMuxerV2(
	quicSession quic.Connection,
	log *zerolog.Logger,
	sessionDemuxChan chan<- *SessionDatagram,
	packetDemuxChan chan<- []byte) *DatagramMuxerV2 {
	logger := log.With().Uint8("datagramVersion", 2).Logger()
	return &DatagramMuxerV2{
		session:          quicSession,
		logger:           &logger,
		sessionDemuxChan: sessionDemuxChan,
		packetDemuxChan:  packetDemuxChan,
	}
}

// MuxSession suffix the session ID and datagram version to the payload so the other end of the QUIC connection can
// demultiplex the payload from multiple datagram sessions
func (dm *DatagramMuxerV2) MuxSession(sessionID uuid.UUID, payload []byte) error {
	if len(payload) > dm.mtu() {
		// TODO: TUN-5302 return ICMP packet too big message
		return fmt.Errorf("origin UDP payload has %d bytes, which exceeds transport MTU %d", len(payload), dm.mtu())
	}
	msgWithID, err := suffixSessionID(sessionID, payload)
	if err != nil {
		return errors.Wrap(err, "Failed to suffix session ID to datagram, it will be dropped")
	}
	msgWithIDAndType, err := suffixType(msgWithID, udp)
	if err != nil {
		return errors.Wrap(err, "Failed to suffix datagram type, it will be dropped")
	}
	if err := dm.session.SendMessage(msgWithIDAndType); err != nil {
		return errors.Wrap(err, "Failed to send datagram back to edge")
	}
	return nil
}

// MuxPacket suffix the datagram type to the packet. The other end of the QUIC connection can demultiplex by parsing
// the payload as IP and look at the source and destination.
func (dm *DatagramMuxerV2) MuxPacket(packet []byte) error {
	payloadWithVersion, err := suffixType(packet, ip)
	if err != nil {
		return errors.Wrap(err, "Failed to suffix datagram type, it will be dropped")
	}
	if err := dm.session.SendMessage(payloadWithVersion); err != nil {
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

func (dm *DatagramMuxerV2) demux(ctx context.Context, msgWithType []byte) error {
	if len(msgWithType) < 1 {
		return fmt.Errorf("QUIC datagram should have at least 1 byte")
	}
	msgType := datagramV2Type(msgWithType[len(msgWithType)-1])
	msg := msgWithType[0 : len(msgWithType)-1]
	switch msgType {
	case udp:
		sessionID, payload, err := extractSessionID(msg)
		if err != nil {
			return err
		}
		sessionDatagram := SessionDatagram{
			ID:      sessionID,
			Payload: payload,
		}
		select {
		case dm.sessionDemuxChan <- &sessionDatagram:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	case ip:
		select {
		case dm.packetDemuxChan <- msg:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		return fmt.Errorf("Unexpected datagram type %d", msgType)
	}
}
