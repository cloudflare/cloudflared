package sentry

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

type LogLevel string

const (
	LogLevelTrace LogLevel = "trace"
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
	LogLevelFatal LogLevel = "fatal"
)

const (
	LogSeverityTrace   int = 1
	LogSeverityDebug   int = 5
	LogSeverityInfo    int = 9
	LogSeverityWarning int = 13
	LogSeverityError   int = 17
	LogSeverityFatal   int = 21
)

type sentryLogger struct {
	ctx               context.Context
	hub               *Hub
	attributes        map[string]attribute.Value
	defaultAttributes map[string]attribute.Value
	mu                sync.RWMutex
}

type logEntry struct {
	logger      *sentryLogger
	ctx         context.Context
	level       LogLevel
	severity    int
	attributes  map[string]attribute.Value
	shouldPanic bool
	shouldFatal bool
}

// NewLogger returns a Logger that emits logs to Sentry. If logging is turned off, all logs get discarded.
func NewLogger(ctx context.Context) Logger { // nolint: dupl
	var hub *Hub
	hub = GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}

	client := hub.Client()
	if client != nil && client.options.EnableLogs {
		// Build default attrs
		serverAddr := client.options.ServerName
		if serverAddr == "" {
			serverAddr, _ = os.Hostname()
		}

		defaults := map[string]string{
			"sentry.release":        client.options.Release,
			"sentry.environment":    client.options.Environment,
			"sentry.server.address": serverAddr,
			"sentry.sdk.name":       client.sdkIdentifier,
			"sentry.sdk.version":    client.sdkVersion,
		}

		defaultAttrs := make(map[string]attribute.Value, len(defaults))
		for k, v := range defaults {
			if v != "" {
				defaultAttrs[k] = attribute.StringValue(v)
			}
		}

		return &sentryLogger{
			ctx:               ctx,
			hub:               hub,
			attributes:        make(map[string]attribute.Value),
			defaultAttributes: defaultAttrs,
			mu:                sync.RWMutex{},
		}
	}

	debuglog.Println("fallback to noopLogger: enableLogs disabled")
	return &noopLogger{}
}

func (l *sentryLogger) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	l.Info().Emit(msg)
	return len(p), nil
}

func (l *sentryLogger) log(ctx context.Context, level LogLevel, severity int, message string, entryAttrs map[string]attribute.Value, args ...interface{}) {
	if message == "" {
		return
	}

	hub := hubFromContexts(ctx, l.ctx)
	if hub == nil {
		hub = l.hub
	}
	client := hub.Client()
	if client == nil {
		return
	}

	scope := hub.Scope()
	traceID, spanID := resolveTrace(scope, ctx, l.ctx)

	// Pre-allocate with capacity hint to avoid map growth reallocations
	estimatedCap := len(l.defaultAttributes) + len(entryAttrs) + len(args) + 8 // scope ~3 + instance ~5
	attrs := make(map[string]attribute.Value, estimatedCap)

	// attribute precedence: default -> scope -> instance (from SetAttrs) -> entry-specific
	for k, v := range l.defaultAttributes {
		attrs[k] = v
	}
	scope.populateAttrs(attrs)

	l.mu.RLock()
	for k, v := range l.attributes {
		attrs[k] = v
	}
	l.mu.RUnlock()

	for k, v := range entryAttrs {
		attrs[k] = v
	}

	if len(args) > 0 {
		attrs["sentry.message.template"] = attribute.StringValue(message)
		for i, p := range args {
			attrs[fmt.Sprintf("sentry.message.parameters.%d", i)] = attribute.StringValue(fmt.Sprintf("%+v", p))
		}
	}

	log := &Log{
		Timestamp:  time.Now(),
		TraceID:    traceID,
		SpanID:     spanID,
		Level:      level,
		Severity:   severity,
		Body:       fmt.Sprintf(message, args...),
		Attributes: attrs,
	}

	client.captureLog(log, scope)
	if client.options.Debug {
		debuglog.Printf(message, args...)
	}
}

func (l *sentryLogger) SetAttributes(attrs ...attribute.Builder) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, a := range attrs {
		if a.Value.Type() == attribute.INVALID {
			debuglog.Printf("invalid attribute: %v", a)
			continue
		}
		l.attributes[a.Key] = a.Value
	}
}

func (l *sentryLogger) Trace() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        l.ctx,
		level:      LogLevelTrace,
		severity:   LogSeverityTrace,
		attributes: make(map[string]attribute.Value),
	}
}

func (l *sentryLogger) Debug() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        l.ctx,
		level:      LogLevelDebug,
		severity:   LogSeverityDebug,
		attributes: make(map[string]attribute.Value),
	}
}

func (l *sentryLogger) Info() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        l.ctx,
		level:      LogLevelInfo,
		severity:   LogSeverityInfo,
		attributes: make(map[string]attribute.Value),
	}
}

func (l *sentryLogger) Warn() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        l.ctx,
		level:      LogLevelWarn,
		severity:   LogSeverityWarning,
		attributes: make(map[string]attribute.Value),
	}
}

func (l *sentryLogger) Error() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        l.ctx,
		level:      LogLevelError,
		severity:   LogSeverityError,
		attributes: make(map[string]attribute.Value),
	}
}

func (l *sentryLogger) Fatal() LogEntry {
	return &logEntry{
		logger:      l,
		ctx:         l.ctx,
		level:       LogLevelFatal,
		severity:    LogSeverityFatal,
		attributes:  make(map[string]attribute.Value),
		shouldFatal: true,
	}
}

func (l *sentryLogger) Panic() LogEntry {
	return &logEntry{
		logger:      l,
		ctx:         l.ctx,
		level:       LogLevelFatal,
		severity:    LogSeverityFatal,
		attributes:  make(map[string]attribute.Value),
		shouldPanic: true,
	}
}

func (l *sentryLogger) LFatal() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        l.ctx,
		level:      LogLevelFatal,
		severity:   LogSeverityFatal,
		attributes: make(map[string]attribute.Value),
	}
}

func (l *sentryLogger) GetCtx() context.Context {
	return l.ctx
}

func (e *logEntry) WithCtx(ctx context.Context) LogEntry {
	return &logEntry{
		logger:      e.logger,
		ctx:         ctx,
		level:       e.level,
		severity:    e.severity,
		attributes:  maps.Clone(e.attributes),
		shouldPanic: e.shouldPanic,
		shouldFatal: e.shouldFatal,
	}
}

func (e *logEntry) String(key, value string) LogEntry {
	e.attributes[key] = attribute.StringValue(value)
	return e
}

func (e *logEntry) Int(key string, value int) LogEntry {
	e.attributes[key] = attribute.Int64Value(int64(value))
	return e
}

func (e *logEntry) Int64(key string, value int64) LogEntry {
	e.attributes[key] = attribute.Int64Value(value)
	return e
}

func (e *logEntry) Float64(key string, value float64) LogEntry {
	e.attributes[key] = attribute.Float64Value(value)
	return e
}

func (e *logEntry) Bool(key string, value bool) LogEntry {
	e.attributes[key] = attribute.BoolValue(value)
	return e
}

// Uint64 adds uint64 attributes to the log entry.
//
// This method is intentionally not part of the LogEntry interface to avoid exposing uint64 in the public API.
func (e *logEntry) Uint64(key string, value uint64) LogEntry {
	e.attributes[key] = attribute.Uint64Value(value)
	return e
}

func (e *logEntry) Emit(args ...interface{}) {
	e.logger.log(e.ctx, e.level, e.severity, fmt.Sprint(args...), e.attributes)

	if e.level == LogLevelFatal {
		if e.shouldPanic {
			panic(fmt.Sprint(args...))
		}
		if e.shouldFatal {
			os.Exit(1)
		}
	}
}

func (e *logEntry) Emitf(format string, args ...interface{}) {
	e.logger.log(e.ctx, e.level, e.severity, format, e.attributes, args...)

	if e.level == LogLevelFatal {
		if e.shouldPanic {
			formattedMessage := fmt.Sprintf(format, args...)
			panic(formattedMessage)
		}
		if e.shouldFatal {
			os.Exit(1)
		}
	}
}
