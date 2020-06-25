package sshlog

import (
	"time"

	"github.com/cloudflare/cloudflared/logger"
	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/pogs"
)

// SessionLogger will buffer and write events to disk using capnp proto for session replay
type SessionLogger struct {
	logger  *Logger
	encoder *capnp.Encoder
}

type sessionLogData struct {
	Timestamp string // The UTC timestamp of when the log occurred
	Content   []byte // The shell output
}

// NewSessionLogger creates a new session logger by encapsulating a Logger object and writing capnp encoded messages to it
func NewSessionLogger(filename string, logger logger.Service, flushInterval time.Duration, maxFileSize int64) (*SessionLogger, error) {
	l, err := NewLogger(filename, logger, flushInterval, maxFileSize)
	if err != nil {
		return nil, err
	}
	sessionLogger := &SessionLogger{
		logger:  l,
		encoder: capnp.NewEncoder(l),
	}
	return sessionLogger, nil
}

// Writes to a log buffer. Implements the io.Writer interface.
func (l *SessionLogger) Write(p []byte) (n int, err error) {
	return l.writeSessionLog(&sessionLogData{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Content:   p,
	})
}

// Close drains anything left in the buffer and cleans up any resources still
// in use.
func (l *SessionLogger) Close() error {
	return l.logger.Close()
}

func (l *SessionLogger) writeSessionLog(p *sessionLogData) (int, error) {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return 0, err
	}
	log, err := NewRootSessionLog(seg)
	if err != nil {
		return 0, err
	}
	log.SetTimestamp(p.Timestamp)
	log.SetContent(p.Content)

	if err := l.encoder.Encode(msg); err != nil {
		return 0, err
	}
	return len(p.Content), nil
}

func unmarshalSessionLog(s SessionLog) (*sessionLogData, error) {
	p := new(sessionLogData)
	err := pogs.Extract(p, SessionLog_TypeID, s.Struct)
	return p, err
}
