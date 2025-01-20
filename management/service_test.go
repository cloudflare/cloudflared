package management

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"

	"github.com/cloudflare/cloudflared/internal/test"
)

var (
	noopLogger         = zerolog.New(io.Discard)
	managementHostname = "https://management.argotunnel.com"
)

func TestDisableDiagnosticRoutes(t *testing.T) {
	mgmt := New("management.argotunnel.com", false, "1.1.1.1:80", uuid.Nil, "", &noopLogger, nil)
	for _, path := range []string{"/metrics", "/debug/pprof/goroutine", "/debug/pprof/heap"} {
		t.Run(strings.Replace(path, "/", "_", -1), func(t *testing.T) {
			req := httptest.NewRequest("GET", managementHostname+path+"?access_token="+validToken, nil)
			recorder := httptest.NewRecorder()
			mgmt.ServeHTTP(recorder, req)
			resp := recorder.Result()
			require.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

func TestReadEventsLoop(t *testing.T) {
	sentEvent := EventStartStreaming{
		ClientEvent: ClientEvent{Type: StartStreaming},
	}
	client, server := test.WSPipe(nil, nil)
	client.CloseRead(context.Background())
	defer func() {
		client.Close(websocket.StatusInternalError, "")
	}()
	go func() {
		err := WriteEvent(client, context.Background(), &sentEvent)
		require.NoError(t, err)
	}()
	m := ManagementService{
		log: &noopLogger,
	}
	events := make(chan *ClientEvent)
	go m.readEvents(server, context.Background(), events)
	event := <-events
	require.Equal(t, sentEvent.Type, event.Type)
	server.Close(websocket.StatusInternalError, "")
}

func TestReadEventsLoop_ContextCancelled(t *testing.T) {
	client, server := test.WSPipe(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	client.CloseRead(ctx)
	defer func() {
		client.Close(websocket.StatusInternalError, "")
	}()
	m := ManagementService{
		log: &noopLogger,
	}
	events := make(chan *ClientEvent)
	go func() {
		time.Sleep(time.Second)
		cancel()
	}()
	// Want to make sure this function returns when context is cancelled
	m.readEvents(server, ctx, events)
	server.Close(websocket.StatusInternalError, "")
}

func TestCanStartStream_NoSessions(t *testing.T) {
	m := ManagementService{
		log: &noopLogger,
		logger: &Logger{
			Log: &noopLogger,
		},
	}
	_, cancel := context.WithCancel(context.Background())
	session := newSession(0, actor{}, cancel)
	assert.True(t, m.canStartStream(session))
}

func TestCanStartStream_ExistingSessionDifferentActor(t *testing.T) {
	m := ManagementService{
		log: &noopLogger,
		logger: &Logger{
			Log: &noopLogger,
		},
	}
	_, cancel := context.WithCancel(context.Background())
	session1 := newSession(0, actor{ID: "test"}, cancel)
	assert.True(t, m.canStartStream(session1))
	m.logger.Listen(session1)
	assert.True(t, session1.Active())

	// Try another session
	session2 := newSession(0, actor{ID: "test2"}, cancel)
	assert.Equal(t, 1, m.logger.ActiveSessions())
	assert.False(t, m.canStartStream(session2))

	// Close session1
	m.logger.Remove(session1)
	assert.True(t, session1.Active()) // Remove doesn't stop a session
	session1.Stop()
	assert.False(t, session1.Active())
	assert.Equal(t, 0, m.logger.ActiveSessions())

	// Try session2 again
	assert.True(t, m.canStartStream(session2))
}

func TestCanStartStream_ExistingSessionSameActor(t *testing.T) {
	m := ManagementService{
		log: &noopLogger,
		logger: &Logger{
			Log: &noopLogger,
		},
	}
	actor := actor{ID: "test"}
	_, cancel := context.WithCancel(context.Background())
	session1 := newSession(0, actor, cancel)
	assert.True(t, m.canStartStream(session1))
	m.logger.Listen(session1)
	assert.True(t, session1.Active())

	// Try another session
	session2 := newSession(0, actor, cancel)
	assert.Equal(t, 1, m.logger.ActiveSessions())
	assert.True(t, m.canStartStream(session2))
	// session1 is removed and stopped
	assert.Equal(t, 0, m.logger.ActiveSessions())
	assert.False(t, session1.Active())
}
