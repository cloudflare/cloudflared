package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestNewCfTracer(t *testing.T) {
	log := zerolog.Nop()
	req := httptest.NewRequest("GET", "http://localhost", nil)
	req.Header.Add(TracerContextName, "14cb070dde8e51fc5ae8514e69ba42ca:b38f1bf5eae406f3:0:1")
	tr := NewTracedHTTPRequest(req, 0, &log)
	assert.NotNil(t, tr)
	assert.IsType(t, tracesdk.NewTracerProvider(), tr.TracerProvider)
	assert.IsType(t, &InMemoryOtlpClient{}, tr.exporter)
}

func TestNewCfTracerMultiple(t *testing.T) {
	log := zerolog.Nop()
	req := httptest.NewRequest("GET", "http://localhost", nil)
	req.Header.Add(TracerContextName, "1241ce3ecdefc68854e8514e69ba42ca:b38f1bf5eae406f3:0:1")
	req.Header.Add(TracerContextName, "14cb070dde8e51fc5ae8514e69ba42ca:b38f1bf5eae406f3:0:1")
	tr := NewTracedHTTPRequest(req, 0, &log)
	assert.NotNil(t, tr)
	assert.IsType(t, tracesdk.NewTracerProvider(), tr.TracerProvider)
	assert.IsType(t, &InMemoryOtlpClient{}, tr.exporter)
}

func TestNewCfTracerNilHeader(t *testing.T) {
	log := zerolog.Nop()
	req := httptest.NewRequest("GET", "http://localhost", nil)
	req.Header[http.CanonicalHeaderKey(TracerContextName)] = nil
	tr := NewTracedHTTPRequest(req, 0, &log)
	assert.NotNil(t, tr)
	assert.IsType(t, trace.NewNoopTracerProvider(), tr.TracerProvider)
	assert.IsType(t, &NoopOtlpClient{}, tr.exporter)
}

func TestNewCfTracerInvalidHeaders(t *testing.T) {
	log := zerolog.Nop()
	req := httptest.NewRequest("GET", "http://localhost", nil)
	for _, test := range [][]string{nil, {""}} {
		req.Header[http.CanonicalHeaderKey(TracerContextName)] = test
		tr := NewTracedHTTPRequest(req, 0, &log)
		assert.NotNil(t, tr)
		assert.IsType(t, trace.NewNoopTracerProvider(), tr.TracerProvider)
		assert.IsType(t, &NoopOtlpClient{}, tr.exporter)
	}
}

func TestAddingSpansWithNilMap(t *testing.T) {
	log := zerolog.Nop()
	req := httptest.NewRequest("GET", "http://localhost", nil)
	req.Header.Add(TracerContextName, "14cb070dde8e51fc5ae8514e69ba42ca:b38f1bf5eae406f3:0:1")
	tr := NewTracedHTTPRequest(req, 0, &log)

	exporter := tr.exporter.(*InMemoryOtlpClient)

	// add fake spans
	spans := createResourceSpans([]*tracepb.Span{createOtlpSpan(traceId)})
	err := exporter.UploadTraces(context.Background(), spans)
	assert.NoError(t, err)

	// a panic shouldn't occur
	tr.AddSpans(nil)
}

func FuzzNewIdentity(f *testing.F) {
	f.Fuzz(func(t *testing.T, trace string) {
		_, _ = NewIdentity(trace)
	})
}
