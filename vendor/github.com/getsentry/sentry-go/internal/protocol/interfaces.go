package protocol

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// TelemetryItem represents any telemetry data that can be stored in buffers and sent to Sentry.
// This is the base interface that all telemetry items must implement.
type TelemetryItem interface {
	// GetCategory returns the rate limit category for this item.
	GetCategory() ratelimit.Category

	// GetEventID returns the event ID for this item.
	GetEventID() string

	// GetSdkInfo returns SDK information for the envelope header.
	GetSdkInfo() *SdkInfo

	// GetDynamicSamplingContext returns trace context for the envelope header.
	GetDynamicSamplingContext() map[string]string
}

// EnvelopeItemConvertible represents items that can be converted directly to envelope items.
type EnvelopeItemConvertible interface {
	TelemetryItem

	// ToEnvelopeItem converts the item to a Sentry envelope item.
	ToEnvelopeItem() (*EnvelopeItem, error)
}

// TelemetryTransport represents the envelope-first transport interface.
// This interface is designed for the telemetry buffer system and provides
// non-blocking sends with backpressure signals.
type TelemetryTransport interface {
	// SendEnvelope sends an envelope to Sentry. Returns immediately with
	// backpressure error if the queue is full.
	SendEnvelope(envelope *Envelope) error

	// HasCapacity reports whether the transport has capacity to accept at least one more envelope.
	HasCapacity() bool

	// IsRateLimited checks if a specific category is currently rate limited
	IsRateLimited(category ratelimit.Category) bool

	// Flush waits for all pending envelopes to be sent, with timeout
	Flush(timeout time.Duration) bool

	// FlushWithContext waits for all pending envelopes to be sent
	FlushWithContext(ctx context.Context) bool

	// Close shuts down the transport gracefully
	Close()
}
