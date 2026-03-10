package telemetry

// TraceAware is implemented by items that can expose a trace ID.
// BucketedBuffer uses this to group items by trace.
type TraceAware interface {
	GetTraceID() (string, bool)
}
