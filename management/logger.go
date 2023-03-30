package management

import (
	"os"
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/rs/zerolog"
)

var json = jsoniter.ConfigFastest

const (
	// Indicates how many log messages the listener will hold before dropping.
	// Provides a throttling mechanism to drop latest messages if the sender
	// can't keep up with the influx of log messages.
	logWindow = 30
)

// ZeroLogEvent is the json structure that zerolog stores it's events as
type ZeroLogEvent struct {
	Time    string       `json:"time,omitempty"`
	Level   LogLevel     `json:"level,omitempty"`
	Type    LogEventType `json:"type,omitempty"`
	Message string       `json:"message,omitempty"`
}

// Logger manages the number of management streaming log sessions
type Logger struct {
	sessions []*Session
	mu       sync.RWMutex

	// Unique logger that isn't a io.Writer of the list of zerolog writers. This helps prevent management log
	// statements from creating infinite recursion to export messages to a session and allows basic debugging and
	// error statements to be issued in the management code itself.
	Log *zerolog.Logger
}

func NewLogger() *Logger {
	log := zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}).With().Timestamp().Logger().Level(zerolog.DebugLevel)
	return &Logger{
		Log: &log,
	}
}

type LoggerListener interface {
	Listen() *Session
	Close(*Session)
}

type Session struct {
	// Buffered channel that holds the recent log events
	listener chan *ZeroLogEvent
	// Types of log events that this session will provide through the listener
	filters []LogEventType
}

func newListener(size int) *Session {
	return &Session{
		listener: make(chan *ZeroLogEvent, size),
		filters:  []LogEventType{},
	}
}

// Listen creates a new Session that will append filtered log events as they are created.
func (l *Logger) Listen() *Session {
	l.mu.Lock()
	defer l.mu.Unlock()
	listener := newListener(logWindow)
	l.sessions = append(l.sessions, listener)
	return listener
}

// Close will remove a Session from the available sessions that were receiving log events.
func (l *Logger) Close(session *Session) {
	l.mu.Lock()
	defer l.mu.Unlock()
	index := -1
	for i, v := range l.sessions {
		if v == session {
			index = i
			break
		}
	}
	if index == -1 {
		// Not found
		return
	}
	copy(l.sessions[index:], l.sessions[index+1:])
	l.sessions = l.sessions[:len(l.sessions)-1]
}

// Write will write the log event to all sessions that have available capacity. For those that are full, the message
// will be dropped.
// This function is the interface that zerolog expects to call when a log event is to be written out.
func (l *Logger) Write(p []byte) (int, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	// return early if no active sessions
	if len(l.sessions) == 0 {
		return len(p), nil
	}
	var event ZeroLogEvent
	iter := json.BorrowIterator(p)
	defer json.ReturnIterator(iter)
	iter.ReadVal(&event)
	if iter.Error != nil {
		l.Log.Debug().Msg("unable to unmarshal log event")
		return len(p), nil
	}
	for _, listener := range l.sessions {
		// no filters means all types are allowed
		if len(listener.filters) != 0 {
			valid := false
			// make sure listener is subscribed to this event type
			for _, t := range listener.filters {
				if t == event.Type {
					valid = true
					break
				}
			}
			if !valid {
				continue
			}
		}

		select {
		case listener.listener <- &event:
		default:
			// buffer is full, discard
		}
	}
	return len(p), nil
}

func (l *Logger) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	return l.Write(p)
}
