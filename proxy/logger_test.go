package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogHTTPRequestRedactsHeaderValues(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "http://example.com/path", nil)
	request.Header = http.Header{
		"Authorization": {"Bearer test-credential"},
		"Cookie":        {"session=test-session"},
		"X-Request-Id":  {"request-123", "request-456"},
	}
	originalHeaders := request.Header.Clone()

	var output bytes.Buffer
	logger := zerolog.New(&output).Level(zerolog.DebugLevel)
	logHTTPRequest(&logger, request)

	var entry struct {
		Level   string              `json:"level"`
		Headers map[string][]string `json:"headers"`
		Message string              `json:"message"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	assert.Equal(t, "debug", entry.Level)
	assert.Equal(t, "GET http://example.com/path HTTP/1.1", entry.Message)
	assert.Equal(t, map[string][]string{
		"Authorization": {"[redacted]"},
		"Cookie":        {"[redacted]"},
		"X-Request-Id":  {"[redacted]", "[redacted]"},
	}, entry.Headers)
	assert.Equal(t, originalHeaders, request.Header)
	assert.NotContains(t, output.String(), "test-credential")
	assert.NotContains(t, output.String(), "test-session")
	assert.NotContains(t, output.String(), "request-123")
}
