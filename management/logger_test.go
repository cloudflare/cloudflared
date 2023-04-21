package management

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No listening sessions will not write to the channel
func TestLoggerWrite_NoSessions(t *testing.T) {
	logger := NewLogger()
	zlog := zerolog.New(logger).With().Timestamp().Logger().Level(zerolog.InfoLevel)

	zlog.Info().Msg("hello")
}

// Validate that the session receives the event
func TestLoggerWrite_OneSession(t *testing.T) {
	logger := NewLogger()
	zlog := zerolog.New(logger).With().Timestamp().Logger().Level(zerolog.InfoLevel)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	session := newSession(logWindow, actor{ID: actorID}, cancel)
	logger.Listen(session)
	defer logger.Remove(session)
	assert.Equal(t, 1, logger.ActiveSessions())
	assert.Equal(t, session, logger.ActiveSession(actor{ID: actorID}))
	zlog.Info().Int(EventTypeKey, int(HTTP)).Msg("hello")
	select {
	case event := <-session.listener:
		assert.NotEmpty(t, event.Time)
		assert.Equal(t, "hello", event.Message)
		assert.Equal(t, Info, event.Level)
		assert.Equal(t, HTTP, event.Event)
	default:
		assert.Fail(t, "expected an event to be in the listener")
	}
}

// Validate all sessions receive the same event
func TestLoggerWrite_MultipleSessions(t *testing.T) {
	logger := NewLogger()
	zlog := zerolog.New(logger).With().Timestamp().Logger().Level(zerolog.InfoLevel)
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	session1 := newSession(logWindow, actor{}, cancel)
	logger.Listen(session1)
	defer logger.Remove(session1)
	assert.Equal(t, 1, logger.ActiveSessions())

	session2 := newSession(logWindow, actor{}, cancel)
	logger.Listen(session2)
	assert.Equal(t, 2, logger.ActiveSessions())

	zlog.Info().Int(EventTypeKey, int(HTTP)).Msg("hello")
	for _, session := range []*session{session1, session2} {
		select {
		case event := <-session.listener:
			assert.NotEmpty(t, event.Time)
			assert.Equal(t, "hello", event.Message)
			assert.Equal(t, Info, event.Level)
			assert.Equal(t, HTTP, event.Event)
		default:
			assert.Fail(t, "expected an event to be in the listener")
		}
	}

	// Close session2 and make sure session1 still receives events
	logger.Remove(session2)
	zlog.Info().Int(EventTypeKey, int(HTTP)).Msg("hello2")
	select {
	case event := <-session1.listener:
		assert.NotEmpty(t, event.Time)
		assert.Equal(t, "hello2", event.Message)
		assert.Equal(t, Info, event.Level)
		assert.Equal(t, HTTP, event.Event)
	default:
		assert.Fail(t, "expected an event to be in the listener")
	}

	// Make sure a held reference to session2 doesn't receive events after being closed
	select {
	case <-session2.listener:
		assert.Fail(t, "An event was not expected to be in the session listener")
	default:
		// pass
	}
}

type mockWriter struct {
	event *Log
	err   error
}

func (m *mockWriter) Write(p []byte) (int, error) {
	m.event, m.err = parseZerologEvent(p)
	return len(p), nil
}

// Validate all event types are set properly
func TestParseZerologEvent_EventTypes(t *testing.T) {
	writer := mockWriter{}
	zlog := zerolog.New(&writer).With().Timestamp().Logger().Level(zerolog.InfoLevel)

	for _, test := range []LogEventType{
		Cloudflared,
		HTTP,
		TCP,
		UDP,
	} {
		t.Run(test.String(), func(t *testing.T) {
			defer func() { writer.err = nil }()
			zlog.Info().Int(EventTypeKey, int(test)).Msg("test")
			require.NoError(t, writer.err)
			require.Equal(t, test, writer.event.Event)
		})
	}

	// Invalid defaults to Cloudflared LogEventType
	t.Run("invalid", func(t *testing.T) {
		defer func() { writer.err = nil }()
		zlog.Info().Str(EventTypeKey, "unknown").Msg("test")
		require.NoError(t, writer.err)
		require.Equal(t, Cloudflared, writer.event.Event)
	})
}

// Validate top-level keys are removed from Fields
func TestParseZerologEvent_Fields(t *testing.T) {
	writer := mockWriter{}
	zlog := zerolog.New(&writer).With().Timestamp().Logger().Level(zerolog.InfoLevel)
	zlog.Info().Int(EventTypeKey, int(Cloudflared)).Str("test", "test").Msg("test message")
	require.NoError(t, writer.err)
	event := writer.event
	require.NotEmpty(t, event.Time)
	require.Equal(t, Cloudflared, event.Event)
	require.Equal(t, Info, event.Level)
	require.Equal(t, "test message", event.Message)

	// Make sure Fields doesn't have other set keys used in the Log struct
	require.NotEmpty(t, event.Fields)
	require.Equal(t, "test", event.Fields["test"])
	require.NotContains(t, event.Fields, EventTypeKey)
	require.NotContains(t, event.Fields, LevelKey)
	require.NotContains(t, event.Fields, MessageKey)
	require.NotContains(t, event.Fields, TimeKey)
}
