package sentry

import (
	"context"
	"fmt"
	"os"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

// Fallback, no-op logger if logging is disabled.
type noopLogger struct{}

// noopLogEntry implements LogEntry for the no-op logger.
type noopLogEntry struct {
	level       LogLevel
	shouldPanic bool
	shouldFatal bool
}

func (n *noopLogEntry) WithCtx(_ context.Context) LogEntry {
	return n
}

func (n *noopLogEntry) String(_, _ string) LogEntry {
	return n
}

func (n *noopLogEntry) Int(_ string, _ int) LogEntry {
	return n
}

func (n *noopLogEntry) Int64(_ string, _ int64) LogEntry {
	return n
}

func (n *noopLogEntry) Float64(_ string, _ float64) LogEntry {
	return n
}

func (n *noopLogEntry) Bool(_ string, _ bool) LogEntry {
	return n
}

func (n *noopLogEntry) Attributes(_ ...attribute.Builder) LogEntry {
	return n
}

func (n *noopLogEntry) Emit(args ...interface{}) {
	debuglog.Printf("Log with level=[%v] is being dropped. Turn on logging via EnableLogs", n.level)
	if n.level == LogLevelFatal {
		if n.shouldPanic {
			panic(args)
		}
		if n.shouldFatal {
			os.Exit(1)
		}
	}
}

func (n *noopLogEntry) Emitf(message string, args ...interface{}) {
	debuglog.Printf("Log with level=[%v] is being dropped. Turn on logging via EnableLogs", n.level)
	if n.level == LogLevelFatal {
		if n.shouldPanic {
			panic(fmt.Sprintf(message, args...))
		}
		if n.shouldFatal {
			os.Exit(1)
		}
	}
}

func (n *noopLogger) GetCtx() context.Context { return context.Background() }

func (*noopLogger) Trace() LogEntry {
	return &noopLogEntry{level: LogLevelTrace}
}

func (*noopLogger) Debug() LogEntry {
	return &noopLogEntry{level: LogLevelDebug}
}

func (*noopLogger) Info() LogEntry {
	return &noopLogEntry{level: LogLevelInfo}
}

func (*noopLogger) Warn() LogEntry {
	return &noopLogEntry{level: LogLevelWarn}
}

func (*noopLogger) Error() LogEntry {
	return &noopLogEntry{level: LogLevelError}
}

func (*noopLogger) Fatal() LogEntry {
	return &noopLogEntry{level: LogLevelFatal, shouldFatal: true}
}

func (*noopLogger) Panic() LogEntry {
	return &noopLogEntry{level: LogLevelFatal, shouldPanic: true}
}

func (*noopLogger) LFatal() LogEntry {
	return &noopLogEntry{level: LogLevelFatal}
}

func (*noopLogger) SetAttributes(...attribute.Builder) {
	debuglog.Printf("No attributes attached. Turn on logging via EnableLogs")
}

func (*noopLogger) Write(_ []byte) (n int, err error) {
	return 0, fmt.Errorf("log with level=[%v] is being dropped. Turn on logging via EnableLogs", LogLevelInfo)
}
