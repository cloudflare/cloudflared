package datagramsession

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/lucas-clemente/quic-go"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	requestChanCapacity = 16
	defaultReqTimeout   = time.Second * 5
)

var (
	errSessionManagerClosed = fmt.Errorf("session manager closed")
)

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
	datagramChan       chan *newDatagram
	closedChan         chan struct{}
	transport          transport
	sessions           map[uuid.UUID]*Session
	log                *zerolog.Logger
	// timeout waiting for an API to finish. This can be overriden in test
	timeout time.Duration
}

func NewManager(transport transport, log *zerolog.Logger) *manager {
	return &manager{
		registrationChan:   make(chan *registerSessionEvent),
		unregistrationChan: make(chan *unregisterSessionEvent),
		// datagramChan is buffered, so it can read more datagrams from transport while the event loop is processing other events
		datagramChan: make(chan *newDatagram, requestChanCapacity),
		closedChan:   make(chan struct{}),
		transport:    transport,
		sessions:     make(map[uuid.UUID]*Session),
		log:          log,
		timeout:      defaultReqTimeout,
	}
}

func (m *manager) UpdateLogger(log *zerolog.Logger) {
	// Benign data race, no problem if the old pointer is read or not concurrently.
	m.log = log
}

func (m *manager) Serve(ctx context.Context) error {
	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		for {
			sessionID, payload, err := m.transport.ReceiveFrom()
			if err != nil {
				if aerr, ok := err.(*quic.ApplicationError); ok && uint64(aerr.ErrorCode) == uint64(quic.NoError) {
					return nil
				} else {
					return err
				}
			}
			datagram := &newDatagram{
				sessionID: sessionID,
				payload:   payload,
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			// Only the event loop routine can update/lookup the sessions map to avoid concurrent access
			// Send the datagram to the event loop. It will find the session to send to
			case m.datagramChan <- datagram:
			}
		}
	})
	errGroup.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case datagram := <-m.datagramChan:
				m.sendToSession(datagram)
			case registration := <-m.registrationChan:
				m.registerSession(ctx, registration)
			// TODO: TUN-5422: Unregister inactive session upon timeout
			case unregistration := <-m.unregistrationChan:
				m.unregisterSession(unregistration)
			}
		}
	})
	err := errGroup.Wait()
	close(m.closedChan)
	m.shutdownSessions(err)
	return err
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
		s.close(closeSessionErr)
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
}

func (m *manager) newSession(id uuid.UUID, dstConn io.ReadWriteCloser) *Session {
	return &Session{
		ID:        id,
		transport: m.transport,
		dstConn:   dstConn,
		// activeAtChan has low capacity. It can be full when there are many concurrent read/write. markActive() will
		// drop instead of blocking because last active time only needs to be an approximation
		activeAtChan: make(chan time.Time, 2),
		// capacity is 2 because close() and dstToTransport routine in Serve() can write to this channel
		closeChan: make(chan error, 2),
		log:       m.log,
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
	}
}

func (m *manager) sendToSession(datagram *newDatagram) {
	session, ok := m.sessions[datagram.sessionID]
	if !ok {
		m.log.Error().Str("sessionID", datagram.sessionID.String()).Msg("session not found")
		return
	}
	// session writes to destination over a connected UDP socket, which should not be blocking, so this call doesn't
	// need to run in another go routine
	_, err := session.transportToDst(datagram.payload)
	if err != nil {
		m.log.Err(err).Str("sessionID", datagram.sessionID.String()).Msg("Failed to write payload to session")
	}
}
