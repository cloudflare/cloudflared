package tracing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/rs/zerolog"
	otelContrib "go.opentelemetry.io/contrib/propagators/jaeger"
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

	TracerContextName         = "cf-trace-id"
	TracerContextNameOverride = "uber-trace-id"

	IntCloudflaredTracingHeader = "cf-int-cloudflared-tracing"

	MaxErrorDescriptionLen = 100
	traceHttpStatusCodeKey = "upstreamStatusCode"

	traceID128bitsWidth = 128 / 4
	separator           = ":"
)

var (
	CanonicalCloudflaredTracingHeader = http.CanonicalHeaderKey(IntCloudflaredTracingHeader)
	Http2TransportAttribute           = trace.WithAttributes(transportAttributeKey.String("http2"))
	QuicTransportAttribute            = trace.WithAttributes(transportAttributeKey.String("quic"))
	HostOSAttribute                   = semconv.HostTypeKey.String(runtime.GOOS)
	HostArchAttribute                 = semconv.HostArchKey.String(runtime.GOARCH)

	otelVersionAttribute        attribute.KeyValue
	hostnameAttribute           attribute.KeyValue
	cloudflaredVersionAttribute attribute.KeyValue
	serviceAttribute            = semconv.ServiceNameKey.String(service)

	transportAttributeKey   = attribute.Key("transport")
	otelVersionAttributeKey = attribute.Key("jaeger.version")

	errNoopTracerProvider = errors.New("noop tracer provider records no spans")
)

func init() {
	// Register the jaeger propagator globally.
	otel.SetTextMapPropagator(otelContrib.Jaeger{})
	otelVersionAttribute = otelVersionAttributeKey.String(fmt.Sprintf("go-otel-%s", otel.Version()))
	if hostname, err := os.Hostname(); err == nil {
		hostnameAttribute = attribute.String("hostname", hostname)
	}
}

func Init(version string) {
	cloudflaredVersionAttribute = semconv.ProcessRuntimeVersionKey.String(version)
}

type TracedHTTPRequest struct {
	*http.Request
	*cfdTracer
	ConnIndex uint8 // The connection index used to proxy the request
}

// NewTracedHTTPRequest creates a new tracer for the current HTTP request context.
func NewTracedHTTPRequest(req *http.Request, connIndex uint8, log *zerolog.Logger) *TracedHTTPRequest {
	ctx, exists := extractTrace(req)
	if !exists {
		return &TracedHTTPRequest{req, &cfdTracer{trace.NewNoopTracerProvider(), &NoopOtlpClient{}, log}, connIndex}
	}
	return &TracedHTTPRequest{req.WithContext(ctx), newCfdTracer(ctx, log), connIndex}
}

func (tr *TracedHTTPRequest) ToTracedContext() *TracedContext {
	return &TracedContext{tr.Context(), tr.cfdTracer}
}

type TracedContext struct {
	context.Context
	*cfdTracer
}

// NewTracedContext creates a new tracer for the current context.
func NewTracedContext(ctx context.Context, traceContext string, log *zerolog.Logger) *TracedContext {
	ctx, exists := extractTraceFromString(ctx, traceContext)
	if !exists {
		return &TracedContext{ctx, &cfdTracer{trace.NewNoopTracerProvider(), &NoopOtlpClient{}, log}}
	}
	return &TracedContext{ctx, newCfdTracer(ctx, log)}
}

type cfdTracer struct {
	trace.TracerProvider
	exporter InMemoryClient
	log      *zerolog.Logger
}

// NewCfdTracer creates a new tracer for the current request context.
func newCfdTracer(ctx context.Context, log *zerolog.Logger) *cfdTracer {
	mc := new(InMemoryOtlpClient)
	exp, err := otlptrace.New(ctx, mc)
	if err != nil {
		return &cfdTracer{trace.NewNoopTracerProvider(), &NoopOtlpClient{}, log}
	}
	tp := tracesdk.NewTracerProvider(
		// We want to dump to in-memory exporter immediately
		tracesdk.WithSyncer(exp),
		// Record information about this application in a Resource.
		tracesdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			serviceAttribute,
			otelVersionAttribute,
			hostnameAttribute,
			cloudflaredVersionAttribute,
			HostOSAttribute,
			HostArchAttribute,
		)),
	)

	return &cfdTracer{tp, mc, log}
}

func (cft *cfdTracer) Tracer() trace.Tracer {
	return cft.TracerProvider.Tracer(tracerInstrumentName)
}

// GetSpans returns the spans as base64 encoded string of protobuf otlp traces.
func (cft *cfdTracer) GetSpans() (enc string) {
	enc, err := cft.exporter.Spans()
	switch err {
	case nil:
		break
	case errNoTraces:
		cft.log.Trace().Err(err).Msgf("expected traces to be available")
		return
	case errNoopTracer:
		return // noop tracer has no traces
	default:
		cft.log.Debug().Err(err)
		return
	}
	return
}

// GetProtoSpans returns the spans as the otlp traces in protobuf byte array.
func (cft *cfdTracer) GetProtoSpans() (proto []byte) {
	proto, err := cft.exporter.ExportProtoSpans()
	switch err {
	case nil:
		break
	case errNoTraces:
		cft.log.Trace().Err(err).Msgf("expected traces to be available")
		return
	case errNoopTracer:
		return // noop tracer has no traces
	default:
		cft.log.Debug().Err(err)
		return
	}
	return
}

// AddSpans assigns spans as base64 encoded protobuf otlp traces to provided
// HTTP headers.
func (cft *cfdTracer) AddSpans(headers http.Header) {
	if headers == nil {
		return
	}

	enc := cft.GetSpans()
	// No need to add header if no traces
	if enc == "" {
		return
	}

	headers[CanonicalCloudflaredTracingHeader] = []string{enc}
}

// End will set the OK status for the span and then end it.
func End(span trace.Span) {
	endSpan(span, -1, codes.Ok, nil)
}

// EndWithErrorStatus will set a status for the span and then end it.
func EndWithErrorStatus(span trace.Span, err error) {
	endSpan(span, -1, codes.Error, err)
}

// EndWithStatusCode will set a status for the span and then end it.
func EndWithStatusCode(span trace.Span, statusCode int) {
	endSpan(span, statusCode, codes.Ok, nil)
}

// EndWithErrorStatus will set a status for the span and then end it.
func endSpan(span trace.Span, upstreamStatusCode int, spanStatusCode codes.Code, err error) {
	if span == nil {
		return
	}

	if upstreamStatusCode > 0 {
		span.SetAttributes(attribute.Int(traceHttpStatusCodeKey, upstreamStatusCode))
	}

	// add error to status buf cap description
	errDescription := ""
	if err != nil {
		errDescription = err.Error()
		l := int(math.Min(float64(len(errDescription)), MaxErrorDescriptionLen))
		errDescription = errDescription[:l]
	}

	span.SetStatus(spanStatusCode, errDescription)
	span.End()
}

// extractTraceFromString will extract the trace information from the provided
// propagated trace string context.
func extractTraceFromString(ctx context.Context, trace string) (context.Context, bool) {
	if trace == "" {
		return ctx, false
	}
	// Jaeger specific separator
	parts := strings.Split(trace, separator)
	if len(parts) != 4 {
		return ctx, false
	}
	if parts[0] == "" {
		return ctx, false
	}
	// Correctly left pad the trace to a length of 32
	if len(parts[0]) < traceID128bitsWidth {
		left := traceID128bitsWidth - len(parts[0])
		parts[0] = strings.Repeat("0", left) + parts[0]
		trace = strings.Join(parts, separator)
	}
	// Override the 'cf-trace-id' as 'uber-trace-id' so the jaeger propagator can extract it.
	traceHeader := map[string]string{TracerContextNameOverride: trace}
	remoteCtx := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(traceHeader))
	return remoteCtx, true
}

// extractTrace attempts to check for a cf-trace-id from a request and return the
// trace context with the provided http.Request.
func extractTrace(req *http.Request) (context.Context, bool) {
	// Only add tracing for requests with appropriately tagged headers
	remoteTraces := req.Header.Values(TracerContextName)
	if len(remoteTraces) <= 0 {
		// Strip the cf-trace-id header
		req.Header.Del(TracerContextName)
		return nil, false
	}

	traceHeader := map[string]string{}
	for _, t := range remoteTraces {
		// Override the 'cf-trace-id' as 'uber-trace-id' so the jaeger propagator can extract it.
		// Last entry wins if multiple provided
		traceHeader[TracerContextNameOverride] = t
	}

	// Strip the cf-trace-id header
	req.Header.Del(TracerContextName)

	if traceHeader[TracerContextNameOverride] == "" {
		return nil, false
	}

	remoteCtx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.MapCarrier(traceHeader))
	return remoteCtx, true
}

func NewNoopSpan() trace.Span {
	return trace.SpanFromContext(nil)
}
