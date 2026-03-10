package sentry

import (
	"context"
	"sync"
	"time"
)

// MockScope implements [Scope] for use in tests.
type MockScope struct {
	breadcrumb      *Breadcrumb
	shouldDropEvent bool
}

func (scope *MockScope) AddBreadcrumb(breadcrumb *Breadcrumb, _ int) {
	scope.breadcrumb = breadcrumb
}

func (scope *MockScope) ApplyToEvent(event *Event, _ *EventHint, _ *Client) *Event {
	if scope.shouldDropEvent {
		return nil
	}
	return event
}

// MockTransport implements [Transport] for use in tests.
type MockTransport struct {
	mu        sync.Mutex
	events    []*Event
	lastEvent *Event
}

func (t *MockTransport) Configure(_ ClientOptions) {}
func (t *MockTransport) SendEvent(event *Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
	t.lastEvent = event
}
func (t *MockTransport) Flush(_ time.Duration) bool {
	return true
}
func (t *MockTransport) FlushWithContext(_ context.Context) bool { return true }
func (t *MockTransport) Events() []*Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.events
}
func (t *MockTransport) Close() {}

// MockLogEntry implements [sentry.LogEntry] for use in tests.
type MockLogEntry struct {
	Attributes map[string]any
}

func NewMockLogEntry() *MockLogEntry {
	return &MockLogEntry{Attributes: make(map[string]any)}
}

func (m *MockLogEntry) WithCtx(_ context.Context) LogEntry { return m }
func (m *MockLogEntry) String(key, value string) LogEntry  { m.Attributes[key] = value; return m }
func (m *MockLogEntry) Int(key string, value int) LogEntry {
	m.Attributes[key] = int64(value)
	return m
}
func (m *MockLogEntry) Int64(key string, value int64) LogEntry {
	m.Attributes[key] = value
	return m
}
func (m *MockLogEntry) Float64(key string, value float64) LogEntry {
	m.Attributes[key] = value
	return m
}
func (m *MockLogEntry) Bool(key string, value bool) LogEntry {
	m.Attributes[key] = value
	return m
}
func (m *MockLogEntry) Emit(...any)          {}
func (m *MockLogEntry) Emitf(string, ...any) {}
