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
	}).With().Timestamp().Logger().Level(zerolog.InfoLevel)
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
	listener chan *Log
	// Types of log events that this session will provide through the listener
	filters []LogEventType
}

func newListener(size int) *Session {
	return &Session{
		listener: make(chan *Log, size),
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
	event, err := parseZerologEvent(p)
	// drop event if unable to parse properly
	if err != nil {
		l.Log.Debug().Msg("unable to parse log event")
		return len(p), nil
	}
	for _, listener := range l.sessions {
		// no filters means all types are allowed
		if len(listener.filters) != 0 {
			valid := false
			// make sure listener is subscribed to this event type
			for _, t := range listener.filters {
				if t == event.Event {
					valid = true
					break
				}
			}
			if !valid {
				continue
			}
		}

		select {
		case listener.listener <- event:
		default:
			// buffer is full, discard
		}
	}
	return len(p), nil
}

func (l *Logger) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	return l.Write(p)
}

func parseZerologEvent(p []byte) (*Log, error) {
	var fields map[string]interface{}
	iter := json.BorrowIterator(p)
	defer json.ReturnIterator(iter)
	iter.ReadVal(&fields)
	if iter.Error != nil {
		return nil, iter.Error
	}
	logTime := time.Now().UTC().Format(zerolog.TimeFieldFormat)
	if t, ok := fields[TimeKey]; ok {
		if t, ok := t.(string); ok {
			logTime = t
		}
	}
	logLevel := Debug
	if level, ok := fields[LevelKey]; ok {
		if level, ok := level.(string); ok {
			logLevel = LogLevel(level)
		}
	}
	// Assume the event type is Cloudflared if unable to parse/find. This could be from log events that haven't
	// yet been tagged with the appropriate EventType yet.
	logEvent := Cloudflared
	e := fields[EventTypeKey]
	if e != nil {
		if eventNumber, ok := e.(float64); ok {
			logEvent = LogEventType(eventNumber)
		}
	}
	logMessage := ""
	if m, ok := fields[MessageKey]; ok {
		if m, ok := m.(string); ok {
			logMessage = m
		}
	}
	event := Log{
		Time:    logTime,
		Level:   logLevel,
		Event:   logEvent,
		Message: logMessage,
	}
	// Remove the keys that have top level keys on Log
	delete(fields, TimeKey)
	delete(fields, LevelKey)
	delete(fields, EventTypeKey)
	delete(fields, MessageKey)
	// The rest of the keys go into the Fields
	event.Fields = fields
	return &event, nil
}
