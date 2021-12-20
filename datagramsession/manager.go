package datagramsession

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	requestChanCapacity = 16
)

// Manager defines the APIs to manage sessions from the same transport.
type Manager interface {
	// Serve starts the event loop
	Serve(ctx context.Context) error
	// RegisterSession starts tracking a session. Caller is responsible for starting the session
	RegisterSession(ctx context.Context, sessionID uuid.UUID, dstConn io.ReadWriteCloser) (*Session, error)
	// UnregisterSession stops tracking the session and terminates it
	UnregisterSession(ctx context.Context, sessionID uuid.UUID) error
}

type manager struct {
	registrationChan   chan *registerSessionEvent
	unregistrationChan chan *unregisterSessionEvent
	datagramChan       chan *newDatagram
	transport          transport
	sessions           map[uuid.UUID]*Session
	log                *zerolog.Logger
}

func NewManager(transport transport, log *zerolog.Logger) Manager {
	return &manager{
		registrationChan:   make(chan *registerSessionEvent),
		unregistrationChan: make(chan *unregisterSessionEvent),
		// datagramChan is buffered, so it can read more datagrams from transport while the event loop is processing other events
		datagramChan: make(chan *newDatagram, requestChanCapacity),
		transport:    transport,
		sessions:     make(map[uuid.UUID]*Session),
		log:          log,
	}
}

func (m *manager) Serve(ctx context.Context) error {
	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		for {
			sessionID, payload, err := m.transport.ReceiveFrom()
			if err != nil {
				return err
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
				return ctx.Err()
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
	return errGroup.Wait()
}

func (m *manager) RegisterSession(ctx context.Context, sessionID uuid.UUID, originProxy io.ReadWriteCloser) (*Session, error) {
	event := newRegisterSessionEvent(sessionID, originProxy)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case m.registrationChan <- event:
		session := <-event.resultChan
		return session, nil
	}
}

func (m *manager) registerSession(ctx context.Context, registration *registerSessionEvent) {
	session := newSession(registration.sessionID, m.transport, registration.originProxy)
	m.sessions[registration.sessionID] = session
	registration.resultChan <- session
}

func (m *manager) UnregisterSession(ctx context.Context, sessionID uuid.UUID) error {
	event := &unregisterSessionEvent{sessionID: sessionID}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.unregistrationChan <- event:
		return nil
	}
}

func (m *manager) unregisterSession(unregistration *unregisterSessionEvent) {
	session, ok := m.sessions[unregistration.sessionID]
	if ok {
		delete(m.sessions, unregistration.sessionID)
		session.close()
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
