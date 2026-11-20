package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/management"
)

// User-Agent prefix that connection.IsLBProbeRequest treats as a Cloudflare load balancer health probe.
const testLBProbeUserAgent = "Mozilla/5.0 (compatible; Cloudflare-Traffic-Manager/1.0; +https://www.cloudflare.com/traffic-manager/; pool-id: 0123456789abcdef)"

func TestNewHTTPLoggerContextFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfRay       string
		userAgent   string
		wantCFRay   string
		wantLBProbe bool
	}{
		{
			name:      "request with Cf-Ray header",
			cfRay:     "8a1b2c3d4e5f6a7b-SJC",
			wantCFRay: "8a1b2c3d4e5f6a7b-SJC",
		},
		{
			name:        "load balancer probe",
			userAgent:   testLBProbeUserAgent,
			wantLBProbe: true,
		},
		{
			name: "request without Cf-Ray header",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			baseLogger := zerolog.New(&output)

			req := httptest.NewRequest(http.MethodGet, "https://example.com/path", nil)
			if tt.cfRay != "" {
				req.Header.Set("Cf-Ray", tt.cfRay)
			}
			if tt.userAgent != "" {
				req.Header.Set("User-Agent", tt.userAgent)
			}

			logger := newHTTPLogger(&baseLogger, 2, req, 3, "http://localhost:8080")
			logger.Error().Msg("request failed")

			decoder := json.NewDecoder(&output)
			decoder.UseNumber()
			var fields map[string]any
			require.NoError(t, decoder.Decode(&fields))

			assert.Equal(t, json.Number(strconv.Itoa(int(management.HTTP))), fields[management.EventTypeKey])
			assert.Equal(t, json.Number("2"), fields[logFieldConnIndex])
			assert.Equal(t, json.Number("3"), fields[logFieldRule])
			assert.Equal(t, "http://localhost:8080", fields[logFieldOriginService])

			if tt.wantCFRay != "" {
				assert.Equal(t, tt.wantCFRay, fields[logFieldCFRay])
			} else {
				assert.NotContains(t, fields, logFieldCFRay)
			}

			if tt.wantLBProbe {
				assert.Equal(t, tt.wantLBProbe, fields[logFieldLBProbe])
			} else {
				assert.NotContains(t, fields, logFieldLBProbe)
			}
		})
	}
}
