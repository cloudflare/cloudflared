package quic

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
)

const (
	MaxDatagramFrameSize = 1220
	sessionIDLen         = len(uuid.UUID{})
)

type DatagramMuxer struct {
	ID      uuid.UUID
	session quic.Session
}

func NewDatagramMuxer(quicSession quic.Session) (*DatagramMuxer, error) {
	muxerID, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	return &DatagramMuxer{
		ID:      muxerID,
		session: quicSession,
	}, nil
}

// SendTo suffix the session ID to the payload so the other end of the QUIC session can demultiplex
// the payload from multiple datagram sessions
func (dm *DatagramMuxer) SendTo(sessionID uuid.UUID, payload []byte) error {
	if len(payload) > MaxDatagramFrameSize-sessionIDLen {
		// TODO: TUN-5302 return ICMP packet too big message
		return fmt.Errorf("origin UDP payload has %d bytes, which exceeds transport MTU %d", len(payload), MaxDatagramFrameSize-sessionIDLen)
	}
	msgWithID, err := SuffixSessionID(sessionID, payload)
	if err != nil {
		return errors.Wrap(err, "Failed to suffix session ID to datagram, it will be dropped")
	}
	if err := dm.session.SendMessage(msgWithID); err != nil {
		return errors.Wrap(err, "Failed to send datagram back to edge")
	}
	return nil
}

// ReceiveFrom extracts datagram session ID, then sends the session ID and payload to session manager
// which determines how to proxy to the origin. It assumes the datagram session has already been
// registered with session manager through other side channel
func (dm *DatagramMuxer) ReceiveFrom() (uuid.UUID, []byte, error) {
	msg, err := dm.session.ReceiveMessage()
	if err != nil {
		return uuid.Nil, nil, err
	}
	return ExtractSessionID(msg)
}

// Each QUIC datagram should be suffixed with session ID.
// ExtractSessionID extracts the session ID and a slice with only the payload
func ExtractSessionID(b []byte) (uuid.UUID, []byte, error) {
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
	if len(b)+len(sessionID) > MaxDatagramFrameSize {
		return nil, fmt.Errorf("datagram size exceed %d", MaxDatagramFrameSize)
	}
	b = append(b, sessionID[:]...)
	return b, nil
}
