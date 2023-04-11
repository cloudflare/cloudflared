package management

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"nhooyr.io/websocket"
)

const (
	// In the current state, an invalid command was provided by the client
	StatusInvalidCommand websocket.StatusCode = 4001
	reasonInvalidCommand                      = "expected start streaming as first event"
	// There are a limited number of available streaming log sessions that cloudflared will service, exceeding this
	// value will return this error to incoming requests.
	StatusSessionLimitExceeded websocket.StatusCode = 4002
	reasonSessionLimitExceeded                      = "limit exceeded for streaming sessions"

	StatusIdleLimitExceeded websocket.StatusCode = 4003
	reasonIdleLimitExceeded                      = "session was idle for too long"
)

type ManagementService struct {
	// The management tunnel hostname
	Hostname string

	log    *zerolog.Logger
	router chi.Router

	// streaming signifies if the service is already streaming logs. Helps limit the number of active users streaming logs
	// from this cloudflared instance.
	streaming atomic.Bool
	// streamingMut is a lock to prevent concurrent requests to start streaming. Utilizing the atomic.Bool is not
	// sufficient to complete this operation since many other checks during an incoming new request are needed
	// to validate this before setting streaming to true.
	streamingMut sync.Mutex
	logger       LoggerListener
}

func New(managementHostname string, log *zerolog.Logger, logger LoggerListener) *ManagementService {
	s := &ManagementService{
		Hostname: managementHostname,
		log:      log,
		logger:   logger,
	}
	r := chi.NewRouter()
	r.Get("/ping", ping)
	r.Head("/ping", ping)
	r.Get("/logs", s.logs)
	s.router = r
	return s
}

func (m *ManagementService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.router.ServeHTTP(w, r)
}

// Management Ping handler
func ping(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
}

// readEvents will loop through all incoming websocket messages from a client and marshal them into the
// proper Event structure and pass through to the events channel. Any invalid messages sent will automatically
// terminate the connection.
func (m *ManagementService) readEvents(c *websocket.Conn, ctx context.Context, events chan<- *ClientEvent) {
	for {
		event, err := ReadClientEvent(c, ctx)
		select {
		case <-ctx.Done():
			return
		default:
			if err != nil {
				// If the client (or the server) already closed the connection, don't attempt to close it again
				if !IsClosed(err, m.log) {
					m.log.Err(err).Send()
					m.log.Err(c.Close(websocket.StatusUnsupportedData, err.Error())).Send()
				}
				// Any errors when reading the messages from the client will close the connection
				return
			}
			events <- event
		}
	}
}

// streamLogs will begin the process of reading from the Session listener and write the log events to the client.
func (m *ManagementService) streamLogs(c *websocket.Conn, ctx context.Context, session *Session) {
	defer m.logger.Close(session)
	for m.streaming.Load() {
		select {
		case <-ctx.Done():
			m.streaming.Store(false)
			return
		case event := <-session.listener:
			err := WriteEvent(c, ctx, &EventLog{
				ServerEvent: ServerEvent{Type: Logs},
				Logs:        []*Log{event},
			})
			if err != nil {
				// If the client (or the server) already closed the connection, don't attempt to close it again
				if !IsClosed(err, m.log) {
					m.log.Err(err).Send()
					m.log.Err(c.Close(websocket.StatusInternalError, err.Error())).Send()
				}
				// Any errors when writing the messages to the client will stop streaming and close the connection
				m.streaming.Store(false)
				return
			}
		default:
			// No messages to send
		}
	}
}

// startStreaming will check the conditions of the request and begin streaming or close the connection for invalid
// requests.
func (m *ManagementService) startStreaming(c *websocket.Conn, ctx context.Context, event *ClientEvent) {
	m.streamingMut.Lock()
	defer m.streamingMut.Unlock()
	// Limits to one user for streaming logs
	if m.streaming.Load() {
		m.log.Warn().
			Msgf("Another management session request was attempted but one session already being served; there is a limit of streaming log sessions to reduce overall performance impact.")
		m.log.Err(c.Close(StatusSessionLimitExceeded, reasonSessionLimitExceeded)).Send()
		return
	}
	// Expect the first incoming request
	startEvent, ok := IntoClientEvent[EventStartStreaming](event, StartStreaming)
	if !ok {
		m.log.Warn().Err(c.Close(StatusInvalidCommand, reasonInvalidCommand)).Msgf("expected start_streaming as first recieved event")
		return
	}
	m.streaming.Store(true)
	listener := m.logger.Listen(startEvent.Filters)
	m.log.Debug().Msgf("Streaming logs")
	go m.streamLogs(c, ctx, listener)
}

// Management Streaming Logs accept handler
func (m *ManagementService) logs(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		m.log.Debug().Msgf("management handshake: %s", err.Error())
		return
	}
	// Make sure the connection is closed if other go routines fail to close the connection after completing.
	defer c.Close(websocket.StatusInternalError, "")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	events := make(chan *ClientEvent)
	go m.readEvents(c, ctx, events)

	// Send a heartbeat ping to hold the connection open even if not streaming.
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()

	// Close the connection if no operation has occurred after the idle timeout.
	idleTimeout := 5 * time.Minute
	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()

	for {
		select {
		case <-ctx.Done():
			m.log.Debug().Msgf("management logs: context cancelled")
			c.Close(websocket.StatusNormalClosure, "context closed")
			return
		case event := <-events:
			switch event.Type {
			case StartStreaming:
				idle.Stop()
				m.startStreaming(c, ctx, event)
				continue
			case StopStreaming:
				idle.Reset(idleTimeout)
				// TODO: limit StopStreaming to only halt streaming for clients that are already streaming
				m.streaming.Store(false)
			case UnknownClientEventType:
				fallthrough
			default:
				// Drop unknown events and close connection
				m.log.Debug().Msgf("unexpected management message received: %s", event.Type)
				// If the client (or the server) already closed the connection, don't attempt to close it again
				if !IsClosed(err, m.log) {
					m.log.Err(err).Err(c.Close(websocket.StatusUnsupportedData, err.Error())).Send()
				}
				return
			}
		case <-ping.C:
			go c.Ping(ctx)
		case <-idle.C:
			c.Close(StatusIdleLimitExceeded, reasonIdleLimitExceeded)
			return
		}
	}
}
