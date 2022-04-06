package tracing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestNewCfTracer(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost", nil)
	req.Header.Add(tracerContextName, "14cb070dde8e51fc5ae8514e69ba42ca:b38f1bf5eae406f3:0:1")
	tr := NewTracedRequest(req)
	assert.NotNil(t, tr)
	assert.IsType(t, tracesdk.NewTracerProvider(), tr.TracerProvider)
	assert.IsType(t, &InMemoryOtlpClient{}, tr.exporter)
}

func TestNewCfTracerMultiple(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost", nil)
	req.Header.Add(tracerContextName, "1241ce3ecdefc68854e8514e69ba42ca:b38f1bf5eae406f3:0:1")
	req.Header.Add(tracerContextName, "14cb070dde8e51fc5ae8514e69ba42ca:b38f1bf5eae406f3:0:1")
	tr := NewTracedRequest(req)
	assert.NotNil(t, tr)
	assert.IsType(t, tracesdk.NewTracerProvider(), tr.TracerProvider)
	assert.IsType(t, &InMemoryOtlpClient{}, tr.exporter)
}

func TestNewCfTracerNilHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost", nil)
	req.Header[http.CanonicalHeaderKey(tracerContextName)] = nil
	tr := NewTracedRequest(req)
	assert.NotNil(t, tr)
	assert.IsType(t, trace.NewNoopTracerProvider(), tr.TracerProvider)
	assert.IsType(t, &NoopOtlpClient{}, tr.exporter)
}

func TestNewCfTracerInvalidHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost", nil)
	for _, test := range [][]string{nil, {""}} {
		req.Header[http.CanonicalHeaderKey(tracerContextName)] = test
		tr := NewTracedRequest(req)
		assert.NotNil(t, tr)
		assert.IsType(t, trace.NewNoopTracerProvider(), tr.TracerProvider)
		assert.IsType(t, &NoopOtlpClient{}, tr.exporter)
	}
}
