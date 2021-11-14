package quic

import (
	"fmt"

	"github.com/google/uuid"
)

const (
	sessionIDLen         = len(uuid.UUID{})
	MaxDatagramFrameSize = 1220
)

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
