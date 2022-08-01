package datagramsession

import (
	"fmt"
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
	err       *errClosedSession
}

// ClosedSessionError represent a condition that closes the session other than I/O
// I/O error is not included, because the side that closes the session is ambiguous.
type errClosedSession struct {
	message  string
	byRemote bool
}

func (sc *errClosedSession) Error() string {
	if sc.byRemote {
		return fmt.Sprintf("session closed by remote due to %s", sc.message)
	} else {
		return fmt.Sprintf("session closed by local due to %s", sc.message)
	}
}
