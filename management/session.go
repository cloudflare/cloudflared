package management

import (
	"context"
	"sync/atomic"
)

const (
	// Indicates how many log messages the listener will hold before dropping.
	// Provides a throttling mechanism to drop latest messages if the sender
	// can't keep up with the influx of log messages.
	logWindow = 30
)

// session captures a streaming logs session for a connection of an actor.
type session struct {
	// Indicates if the session is streaming or not. Modifying this will affect the active session.
	active atomic.Bool
	// Allows the session to control the context of the underlying connection to close it out when done. Mostly
	// used by the LoggerListener to close out and cleanup a session.
	cancel context.CancelFunc
	// Actor who started the session
	actor actor
	// Buffered channel that holds the recent log events
	listener chan *Log
	// Types of log events that this session will provide through the listener
	filters *StreamingFilters
}

// NewSession creates a new session.
func newSession(size int, actor actor, cancel context.CancelFunc) *session {
	s := &session{
		active:   atomic.Bool{},
		cancel:   cancel,
		actor:    actor,
		listener: make(chan *Log, size),
		filters:  &StreamingFilters{},
	}
	return s
}

// Filters assigns the StreamingFilters to the session
func (s *session) Filters(filters *StreamingFilters) {
	if filters != nil {
		s.filters = filters
	} else {
		s.filters = &StreamingFilters{}
	}
}

// Insert attempts to insert the log to the session. If the log event matches the provided session filters, it
// will be applied to the listener.
func (s *session) Insert(log *Log) {
	// Level filters are optional
	if s.filters.Level != nil {
		if *s.filters.Level > log.Level {
			return
		}
	}
	// Event filters are optional
	if len(s.filters.Events) != 0 && !contains(s.filters.Events, log.Event) {
		return
	}
	select {
	case s.listener <- log:
	default:
		// buffer is full, discard
	}
}

// Active returns if the session is active
func (s *session) Active() bool {
	return s.active.Load()
}

// Stop will halt the session
func (s *session) Stop() {
	s.active.Store(false)
}

func contains(array []LogEventType, t LogEventType) bool {
	for _, v := range array {
		if v == t {
			return true
		}
	}
	return false
}
