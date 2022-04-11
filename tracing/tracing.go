package tracing

import (
	"context"
	"errors"
	"net/http"

	"github.com/rs/zerolog"
	otelContrib "go.opentelemetry.io/contrib/propagators/Jaeger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	service              = "cloudflared"
	tracerInstrumentName = "origin"

	tracerContextName         = "cf-trace-id"
	tracerContextNameOverride = "uber-trace-id"

	IntCloudflaredTracingHeader = "cf-int-cloudflared-tracing"
)

var (
	CanonicalCloudflaredTracingHeader = http.CanonicalHeaderKey(IntCloudflaredTracingHeader)
	Http2TransportAttribute           = trace.WithAttributes(TransportAttributeKey.String("http2"))
	QuicTransportAttribute            = trace.WithAttributes(TransportAttributeKey.String("quic"))

	TransportAttributeKey = attribute.Key("transport")
	TrafficAttributeKey   = attribute.Key("traffic")

	errNoopTracerProvider = errors.New("noop tracer provider records no spans")
)

func init() {
	// Register the jaeger propagator globally.
	otel.SetTextMapPropagator(otelContrib.Jaeger{})
}

type TracedRequest struct {
	*http.Request
	trace.TracerProvider
	exporter InMemoryClient
}

// NewTracedRequest creates a new tracer for the current request context.
func NewTracedRequest(req *http.Request) *TracedRequest {
	ctx, exists := extractTrace(req)
	if !exists {
		return &TracedRequest{req, trace.NewNoopTracerProvider(), &NoopOtlpClient{}}
	}
	mc := new(InMemoryOtlpClient)
	exp, err := otlptrace.New(req.Context(), mc)
	if err != nil {
		return &TracedRequest{req, trace.NewNoopTracerProvider(), &NoopOtlpClient{}}
	}
	tp := tracesdk.NewTracerProvider(
		// We want to dump to in-memory exporter immediately
		tracesdk.WithSyncer(exp),
		// Record information about this application in a Resource.
		tracesdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(service),
		)),
	)

	return &TracedRequest{req.WithContext(ctx), tp, mc}
}

func (cft *TracedRequest) Tracer() trace.Tracer {
	return cft.TracerProvider.Tracer(tracerInstrumentName)
}

// Spans returns the spans as base64 encoded protobuf otlp traces.
func (cft *TracedRequest) AddSpans(headers http.Header, log *zerolog.Logger) {
	enc, err := cft.exporter.Spans()
	switch err {
	case nil:
		break
	case errNoTraces:
		log.Error().Err(err).Msgf("expected traces to be available")
		return
	case errNoopTracer:
		return // noop tracer has no traces
	default:
		log.Error().Err(err)
		return
	}
	// No need to add header if no traces
	if enc == "" {
		log.Error().Msgf("no traces provided and no error from exporter")
		return
	}
	headers[CanonicalCloudflaredTracingHeader] = []string{enc}
}

// EndWithStatus will set a status for the span and then end it.
func EndWithStatus(span trace.Span, code codes.Code, status string) {
	if span == nil {
		return
	}
	span.SetStatus(code, status)
	span.End()
}

// extractTrace attempts to check for a cf-trace-id from a request header.
func extractTrace(req *http.Request) (context.Context, bool) {
	// Only add tracing for requests with appropriately tagged headers
	remoteTraces := req.Header.Values(tracerContextName)
	if len(remoteTraces) <= 0 {
		// Strip the cf-trace-id header
		req.Header.Del(tracerContextName)
		return nil, false
	}

	traceHeader := make(map[string]string, 1)
	for _, t := range remoteTraces {
		// Override the 'cf-trace-id' as 'uber-trace-id' so the jaeger propagator can extract it.
		// Last entry wins if multiple provided
		traceHeader[tracerContextNameOverride] = t
	}

	// Strip the cf-trace-id header
	req.Header.Del(tracerContextName)

	if traceHeader[tracerContextNameOverride] == "" {
		return nil, false
	}
	remoteCtx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.MapCarrier(traceHeader))
	return remoteCtx, true
}
