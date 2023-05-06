package quic

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/packet"
)

const (
	sessionIDLen = len(uuid.UUID{})
)

type BaseDatagramMuxer interface {
	// SendToSession suffix the session ID to the payload so the other end of the QUIC connection can demultiplex the
	// payload from multiple datagram sessions.
	SendToSession(session *packet.Session) error
	// ServeReceive starts a loop to receive datagrams from the QUIC connection
	ServeReceive(ctx context.Context) error
}

type DatagramMuxer struct {
	session   quic.Connection
	logger    *zerolog.Logger
	demuxChan chan<- *packet.Session
}

func NewDatagramMuxer(quicSession quic.Connection, log *zerolog.Logger, demuxChan chan<- *packet.Session) *DatagramMuxer {
	logger := log.With().Uint8("datagramVersion", 1).Logger()
	return &DatagramMuxer{
		session:   quicSession,
		logger:    &logger,
		demuxChan: demuxChan,
	}
}

// Maximum application payload to send to / receive from QUIC datagram frame
func (dm *DatagramMuxer) mtu() int {
	return maxDatagramPayloadSize
}

func (dm *DatagramMuxer) SendToSession(session *packet.Session) error {
	if len(session.Payload) > dm.mtu() {
		packetTooBigDropped.Inc()
		return fmt.Errorf("origin UDP payload has %d bytes, which exceeds transport MTU %d", len(session.Payload), dm.mtu())
	}
	payloadWithMetadata, err := SuffixSessionID(session.ID, session.Payload)
	if err != nil {
		return errors.Wrap(err, "Failed to suffix session ID to datagram, it will be dropped")
	}
	if err := dm.session.SendMessage(payloadWithMetadata); err != nil {
		return errors.Wrap(err, "Failed to send datagram back to edge")
	}
	return nil
}

func (dm *DatagramMuxer) ServeReceive(ctx context.Context) error {
	for {
		// Extracts datagram session ID, then sends the session ID and payload to receiver
		// which determines how to proxy to the origin. It assumes the datagram session has already been
		// registered with receiver through other side channel
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

func (dm *DatagramMuxer) demux(ctx context.Context, msg []byte) error {
	sessionID, payload, err := extractSessionID(msg)
	if err != nil {
		return err
	}
	sessionDatagram := packet.Session{
		ID:      sessionID,
		Payload: payload,
	}
	select {
	case dm.demuxChan <- &sessionDatagram:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Each QUIC datagram should be suffixed with session ID.
// extractSessionID extracts the session ID and a slice with only the payload
func extractSessionID(b []byte) (uuid.UUID, []byte, error) {
	msgLen := len(b)
	if msgLen < sessionIDLen {
		return uuid.Nil, nil, fmt.Errorf("session ID has %d bytes, but data only has %d", sessionIDLen, len(b))
	}
	// Parse last 16 bytess as UUID and remove it from slice
	sessionID, err := uuid.FromBytes(b[len(b)-sessionIDLen:])
	if err != nil {
		return uuid.Nil, nil, err
	}
	b = b[:len(b)-sessionIDLen]
	return sessionID, b, nil
}

// SuffixSessionID appends the session ID at the end of the payload. Suffix is more performant than prefix because
// the payload slice might already have enough capacity to append the session ID at the end
func SuffixSessionID(sessionID uuid.UUID, b []byte) ([]byte, error) {
	return suffixMetadata(b, sessionID[:])
}

func suffixMetadata(payload, metadata []byte) ([]byte, error) {
	if len(payload)+len(metadata) > MaxDatagramFrameSize {
		return nil, fmt.Errorf("datagram size exceed %d", MaxDatagramFrameSize)
	}
	return append(payload, metadata...), nil
}
