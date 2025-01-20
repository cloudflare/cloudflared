package datagramsession

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/management"
	"github.com/cloudflare/cloudflared/packet"
)

const (
	requestChanCapacity = 16
	defaultReqTimeout   = time.Second * 5
)

var (
	errSessionManagerClosed = fmt.Errorf("session manager closed")
	LogFieldSessionID       = "sessionID"
)

func FormatSessionID(sessionID uuid.UUID) string {
	sessionIDStr := sessionID.String()
	sessionIDStr = strings.ReplaceAll(sessionIDStr, "-", "")
	return sessionIDStr
}

// Manager defines the APIs to manage sessions from the same transport.
type Manager interface {
	// Serve starts the event loop
	Serve(ctx context.Context) error
	// RegisterSession starts tracking a session. Caller is responsible for starting the session
	RegisterSession(ctx context.Context, sessionID uuid.UUID, dstConn io.ReadWriteCloser) (*Session, error)
	// UnregisterSession stops tracking the session and terminates it
	UnregisterSession(ctx context.Context, sessionID uuid.UUID, message string, byRemote bool) error
	// UpdateLogger updates the logger used by the Manager
	UpdateLogger(log *zerolog.Logger)
}

type manager struct {
	registrationChan   chan *registerSessionEvent
	unregistrationChan chan *unregisterSessionEvent
	sendFunc           transportSender
	receiveChan        <-chan *packet.Session
	closedChan         <-chan struct{}
	sessions           map[uuid.UUID]*Session
	log                *zerolog.Logger
	// timeout waiting for an API to finish. This can be overriden in test
	timeout time.Duration
}

func NewManager(log *zerolog.Logger, sendF transportSender, receiveChan <-chan *packet.Session) *manager {
	return &manager{
		registrationChan:   make(chan *registerSessionEvent),
		unregistrationChan: make(chan *unregisterSessionEvent),
		sendFunc:           sendF,
		receiveChan:        receiveChan,
		closedChan:         make(chan struct{}),
		sessions:           make(map[uuid.UUID]*Session),
		log:                log,
		timeout:            defaultReqTimeout,
	}
}

func (m *manager) UpdateLogger(log *zerolog.Logger) {
	// Benign data race, no problem if the old pointer is read or not concurrently.
	m.log = log
}

func (m *manager) Serve(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			m.shutdownSessions(ctx.Err())
			return ctx.Err()
		// receiveChan is buffered, so the transport can read more datagrams from transport while the event loop is
		// processing other events
		case datagram := <-m.receiveChan:
			m.sendToSession(datagram)
		case registration := <-m.registrationChan:
			m.registerSession(ctx, registration)
		case unregistration := <-m.unregistrationChan:
			m.unregisterSession(unregistration)
		}
	}
}

func (m *manager) shutdownSessions(err error) {
	if err == nil {
		err = errSessionManagerClosed
	}
	closeSessionErr := &errClosedSession{
		message: err.Error(),
		// Usually connection with remote has been closed, so set this to true to skip unregistering from remote
		byRemote: true,
	}
	for _, s := range m.sessions {
		m.unregisterSession(&unregisterSessionEvent{
			sessionID: s.ID,
			err:       closeSessionErr,
		})
	}
}

func (m *manager) RegisterSession(ctx context.Context, sessionID uuid.UUID, originProxy io.ReadWriteCloser) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	event := newRegisterSessionEvent(sessionID, originProxy)
	select {
	case <-ctx.Done():
		m.log.Error().Msg("Datagram session registration timeout")
		return nil, ctx.Err()
	case m.registrationChan <- event:
		session := <-event.resultChan
		return session, nil
	// Once closedChan is closed, manager won't accept more registration because nothing is
	// reading from registrationChan and it's an unbuffered channel
	case <-m.closedChan:
		return nil, errSessionManagerClosed
	}
}

func (m *manager) registerSession(ctx context.Context, registration *registerSessionEvent) {
	session := m.newSession(registration.sessionID, registration.originProxy)
	m.sessions[registration.sessionID] = session
	registration.resultChan <- session
	incrementUDPSessions()
}

func (m *manager) newSession(id uuid.UUID, dstConn io.ReadWriteCloser) *Session {
	logger := m.log.With().
		Int(management.EventTypeKey, int(management.UDP)).
		Str(LogFieldSessionID, FormatSessionID(id)).Logger()
	return &Session{
		ID:       id,
		sendFunc: m.sendFunc,
		dstConn:  dstConn,
		// activeAtChan has low capacity. It can be full when there are many concurrent read/write. markActive() will
		// drop instead of blocking because last active time only needs to be an approximation
		activeAtChan: make(chan time.Time, 2),
		// capacity is 2 because close() and dstToTransport routine in Serve() can write to this channel
		closeChan: make(chan error, 2),
		log:       &logger,
	}
}

func (m *manager) UnregisterSession(ctx context.Context, sessionID uuid.UUID, message string, byRemote bool) error {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	event := &unregisterSessionEvent{
		sessionID: sessionID,
		err: &errClosedSession{
			message:  message,
			byRemote: byRemote,
		},
	}
	select {
	case <-ctx.Done():
		m.log.Error().Msg("Datagram session unregistration timeout")
		return ctx.Err()
	case m.unregistrationChan <- event:
		return nil
	case <-m.closedChan:
		return errSessionManagerClosed
	}
}

func (m *manager) unregisterSession(unregistration *unregisterSessionEvent) {
	session, ok := m.sessions[unregistration.sessionID]
	if ok {
		delete(m.sessions, unregistration.sessionID)
		session.close(unregistration.err)
		decrementUDPActiveSessions()
	}
}

func (m *manager) sendToSession(datagram *packet.Session) {
	session, ok := m.sessions[datagram.ID]
	if !ok {
		m.log.Error().Str(LogFieldSessionID, FormatSessionID(datagram.ID)).Msg("session not found")
		return
	}
	// session writes to destination over a connected UDP socket, which should not be blocking, so this call doesn't
	// need to run in another go routine
	session.transportToDst(datagram.Payload)
}
