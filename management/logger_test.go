package management

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
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

	session := logger.Listen()
	defer logger.Close(session)
	zlog.Info().Int("type", int(HTTP)).Msg("hello")
	select {
	case event := <-session.listener:
		assert.Equal(t, "hello", event.Message)
		assert.Equal(t, LogLevel("info"), event.Level)
		assert.Equal(t, HTTP, event.Type)
	default:
		assert.Fail(t, "expected an event to be in the listener")
	}
}

// Validate all sessions receive the same event
func TestLoggerWrite_MultipleSessions(t *testing.T) {
	logger := NewLogger()
	zlog := zerolog.New(logger).With().Timestamp().Logger().Level(zerolog.InfoLevel)

	session1 := logger.Listen()
	defer logger.Close(session1)
	session2 := logger.Listen()
	zlog.Info().Int("type", int(HTTP)).Msg("hello")
	for _, session := range []*Session{session1, session2} {
		select {
		case event := <-session.listener:
			assert.Equal(t, "hello", event.Message)
			assert.Equal(t, LogLevel("info"), event.Level)
			assert.Equal(t, HTTP, event.Type)
		default:
			assert.Fail(t, "expected an event to be in the listener")
		}
	}

	// Close session2 and make sure session1 still receives events
	logger.Close(session2)
	zlog.Info().Int("type", int(HTTP)).Msg("hello2")
	select {
	case event := <-session1.listener:
		assert.Equal(t, "hello2", event.Message)
		assert.Equal(t, LogLevel("info"), event.Level)
		assert.Equal(t, HTTP, event.Type)
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
