package telemetry

import (
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// Buffer defines the common interface for all buffer implementations.
type Buffer[T any] interface {
	// Core operations
	Offer(item T) bool
	Poll() (T, bool)
	PollBatch(maxItems int) []T
	PollIfReady() []T
	Drain() []T
	Peek() (T, bool)

	// State queries
	Size() int
	Capacity() int
	IsEmpty() bool
	IsFull() bool
	Utilization() float64

	// Flush management
	IsReadyToFlush() bool
	MarkFlushed()

	// Category/Priority
	Category() ratelimit.Category
	Priority() ratelimit.Priority

	// Metrics
	OfferedCount() int64
	DroppedCount() int64
	AcceptedCount() int64
	DropRate() float64
	GetMetrics() BufferMetrics

	// Configuration
	SetDroppedCallback(callback func(item T, reason string))
	Clear()
}
