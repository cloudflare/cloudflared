package management

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	// There is a limited idle time while not actively serving a session for a request before dropping the connection.
	StatusIdleLimitExceeded websocket.StatusCode = 4003
	reasonIdleLimitExceeded                      = "session was idle for too long"
)

var (
	// CORS middleware required to allow dash to access management.argotunnel.com requests
	corsHandler = cors.Handler(cors.Options{
		// Allows for any subdomain of cloudflare.com
		AllowedOrigins: []string{"https://*.cloudflare.com"},
		// Required to present cookies or other authentication across origin boundries
		AllowCredentials: true,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	})
)

type ManagementService struct {
	// The management tunnel hostname
	Hostname string

	// Host details related configurations
	serviceIP string
	clientID  uuid.UUID
	label     string

	// Additional Handlers
	metricsHandler http.Handler

	log    *zerolog.Logger
	router chi.Router

	// streamingMut is a lock to prevent concurrent requests to start streaming. Utilizing the atomic.Bool is not
	// sufficient to complete this operation since many other checks during an incoming new request are needed
	// to validate this before setting streaming to true.
	streamingMut sync.Mutex
	logger       LoggerListener
}

func New(managementHostname string,
	enableDiagServices bool,
	serviceIP string,
	clientID uuid.UUID,
	label string,
	log *zerolog.Logger,
	logger LoggerListener,
) *ManagementService {
	s := &ManagementService{
		Hostname:       managementHostname,
		log:            log,
		logger:         logger,
		serviceIP:      serviceIP,
		clientID:       clientID,
		label:          label,
		metricsHandler: promhttp.Handler(),
	}
	r := chi.NewRouter()
	r.Use(ValidateAccessTokenQueryMiddleware)

	// Default management services
	r.With(corsHandler).Get("/ping", ping)
	r.With(corsHandler).Head("/ping", ping)
	r.Get("/logs", s.logs)
	r.With(corsHandler).Get("/host_details", s.getHostDetails)

	// Diagnostic management services
	if enableDiagServices {
		// Prometheus endpoint
		r.With(corsHandler).Get("/metrics", s.metricsHandler.ServeHTTP)
		// Supports only heap and goroutine
		r.With(corsHandler).Get("/debug/pprof/{profile:heap|goroutine}", pprof.Index)
	}

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

// The response provided by the /host_details endpoint
type getHostDetailsResponse struct {
	ClientID string `json:"connector_id"`
	IP       string `json:"ip,omitempty"`
	HostName string `json:"hostname,omitempty"`
}

func (m *ManagementService) getHostDetails(w http.ResponseWriter, r *http.Request) {
	var getHostDetailsResponse = getHostDetailsResponse{
		ClientID: m.clientID.String(),
	}
	if ip, err := getPrivateIP(m.serviceIP); err == nil {
		getHostDetailsResponse.IP = ip
	}
	getHostDetailsResponse.HostName = m.getLabel()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	json.NewEncoder(w).Encode(getHostDetailsResponse)
}

func (m *ManagementService) getLabel() string {
	if m.label != "" {
		return fmt.Sprintf("custom:%s", m.label)
	}

	// If no label is provided we return the system hostname. This is not
	// a fqdn hostname.
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// Get preferred private ip of this machine
func getPrivateIP(addr string) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().String()
	host, _, err := net.SplitHostPort(localAddr)
	return host, err
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
func (m *ManagementService) streamLogs(c *websocket.Conn, ctx context.Context, session *session) {
	for session.Active() {
		select {
		case <-ctx.Done():
			session.Stop()
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
				session.Stop()
				return
			}
		default:
			// No messages to send
		}
	}
}

// canStartStream will check the conditions of the request and return if the session can begin streaming.
func (m *ManagementService) canStartStream(session *session) bool {
	m.streamingMut.Lock()
	defer m.streamingMut.Unlock()
	// Limits to one actor for streaming logs
	if m.logger.ActiveSessions() > 0 {
		// Allow the same user to preempt their existing session to disconnect their old session and start streaming
		// with this new session instead.
		if existingSession := m.logger.ActiveSession(session.actor); existingSession != nil {
			m.log.Info().
				Msgf("Another management session request for the same actor was requested; the other session will be disconnected to handle the new request.")
			existingSession.Stop()
			m.logger.Remove(existingSession)
			existingSession.cancel()
		} else {
			m.log.Warn().
				Msgf("Another management session request was attempted but one session already being served; there is a limit of streaming log sessions to reduce overall performance impact.")
			return false
		}
	}
	return true
}

// parseFilters will check the ClientEvent for start_streaming and assign filters if provided to the session
func (m *ManagementService) parseFilters(c *websocket.Conn, event *ClientEvent, session *session) bool {
	// Expect the first incoming request
	startEvent, ok := IntoClientEvent[EventStartStreaming](event, StartStreaming)
	if !ok {
		return false
	}
	session.Filters(startEvent.Filters)
	return true
}

// Management Streaming Logs accept handler
func (m *ManagementService) logs(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{
			"*.cloudflare.com",
		},
	})
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

	// Close the connection if no operation has occurred after the idle timeout. The timeout is halted
	// when streaming logs is active.
	idleTimeout := 5 * time.Minute
	idle := time.NewTimer(idleTimeout)
	defer idle.Stop()

	// Fetch the claims from the request context to acquire the actor
	claims, ok := ctx.Value(accessClaimsCtxKey).(*managementTokenClaims)
	if !ok || claims == nil {
		// Typically should never happen as it is provided in the context from the middleware
		m.log.Err(c.Close(websocket.StatusInternalError, "missing access_token")).Send()
		return
	}

	session := newSession(logWindow, claims.Actor, cancel)
	defer m.logger.Remove(session)

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
				// Expect the first incoming request
				startEvent, ok := IntoClientEvent[EventStartStreaming](event, StartStreaming)
				if !ok {
					m.log.Warn().Msgf("expected start_streaming as first recieved event")
					m.log.Err(c.Close(StatusInvalidCommand, reasonInvalidCommand)).Send()
					return
				}
				// Make sure the session can start
				if !m.canStartStream(session) {
					m.log.Err(c.Close(StatusSessionLimitExceeded, reasonSessionLimitExceeded)).Send()
					return
				}
				session.Filters(startEvent.Filters)
				m.logger.Listen(session)
				m.log.Debug().Msgf("Streaming logs")
				go m.streamLogs(c, ctx, session)
				continue
			case StopStreaming:
				idle.Reset(idleTimeout)
				// Stop the current session for the current actor who requested it
				session.Stop()
				m.logger.Remove(session)
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
