package management

import (
	"os"
	"sync"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/rs/zerolog"
)

var json = jsoniter.ConfigFastest

// Logger manages the number of management streaming log sessions
type Logger struct {
	sessions []*session
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
	// ActiveSession returns the first active session for the requested actor.
	ActiveSession(actor) *session
	// ActiveSession returns the count of active sessions.
	ActiveSessions() int
	// Listen appends the session to the list of sessions that receive log events.
	Listen(*session)
	// Remove a session from the available sessions that were receiving log events.
	Remove(*session)
}

func (l *Logger) ActiveSession(actor actor) *session {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, session := range l.sessions {
		if session.actor.ID == actor.ID && session.active.Load() {
			return session
		}
	}
	return nil
}

func (l *Logger) ActiveSessions() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	count := 0
	for _, session := range l.sessions {
		if session.active.Load() {
			count += 1
		}
	}
	return count
}

func (l *Logger) Listen(session *session) {
	l.mu.Lock()
	defer l.mu.Unlock()
	session.active.Store(true)
	l.sessions = append(l.sessions, session)
}

func (l *Logger) Remove(session *session) {
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
	for _, session := range l.sessions {
		session.Insert(event)
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
	// A zerolog Debug event can be created and then an error can be added
	// via .Err(error), if so, we upgrade the level to error.
	if _, hasError := fields["error"]; hasError {
		logLevel = Error
	} else {
		if level, ok := fields[LevelKey]; ok {
			if level, ok := level.(string); ok {
				if logLevel, ok = ParseLogLevel(level); !ok {
					logLevel = Debug
				}
			}
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
