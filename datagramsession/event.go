package datagramsession

import (
	"io"

	"github.com/google/uuid"
)

// registerSessionEvent is an event to start tracking a new session
type registerSessionEvent struct {
	sessionID   uuid.UUID
	originProxy io.ReadWriteCloser
	resultChan  chan *Session
}

func newRegisterSessionEvent(sessionID uuid.UUID, originProxy io.ReadWriteCloser) *registerSessionEvent {
	return &registerSessionEvent{
		sessionID:   sessionID,
		originProxy: originProxy,
		resultChan:  make(chan *Session, 1),
	}
}

// unregisterSessionEvent is an event to stop tracking and terminate the session.
type unregisterSessionEvent struct {
	sessionID uuid.UUID
}

// newDatagram is an event when transport receives new datagram
type newDatagram struct {
	sessionID uuid.UUID
	payload   []byte
}
