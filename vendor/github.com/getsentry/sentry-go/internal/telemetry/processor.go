package telemetry

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// Processor is the top-level object that wraps the scheduler and buffers.
type Processor struct {
	scheduler *Scheduler
}

// NewProcessor creates a new Processor with the given configuration.
func NewProcessor(
	buffers map[ratelimit.Category]Buffer[protocol.TelemetryItem],
	transport protocol.TelemetryTransport,
	dsn *protocol.Dsn,
	sdkInfo *protocol.SdkInfo,
) *Processor {
	scheduler := NewScheduler(buffers, transport, dsn, sdkInfo)
	scheduler.Start()

	return &Processor{
		scheduler: scheduler,
	}
}

// Add adds a TelemetryItem to the appropriate buffer based on its category.
func (b *Processor) Add(item protocol.TelemetryItem) bool {
	return b.scheduler.Add(item)
}

// Flush forces all buffers to flush within the given timeout.
func (b *Processor) Flush(timeout time.Duration) bool {
	return b.scheduler.Flush(timeout)
}

// FlushWithContext flushes with a custom context for cancellation.
func (b *Processor) FlushWithContext(ctx context.Context) bool {
	return b.scheduler.FlushWithContext(ctx)
}

// Close stops the buffer, flushes remaining data, and releases resources.
func (b *Processor) Close(timeout time.Duration) {
	b.scheduler.Stop(timeout)
}
