package tracing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"runtime"

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
			serviceAttribute,
			otelVersionAttribute,
			hostnameAttribute,
			cloudflaredVersionAttribute,
			HostOSAttribute,
			HostArchAttribute,
		)),
	)

	return &TracedRequest{req.WithContext(ctx), tp, mc}
}

func (cft *TracedRequest) Tracer() trace.Tracer {
	return cft.TracerProvider.Tracer(tracerInstrumentName)
}

// Spans returns the spans as base64 encoded protobuf otlp traces.
func (cft *TracedRequest) AddSpans(headers http.Header, log *zerolog.Logger) {
	if headers == nil {
		log.Error().Msgf("provided headers map is nil")
		return
	}

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
