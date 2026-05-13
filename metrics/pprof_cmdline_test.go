package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/diagnostic"
)

func testHandler(t *testing.T) *http.ServeMux {
	t.Helper()

	log := zerolog.Nop()
	return newMetricsHandler(Config{
		DiagnosticHandler: diagnostic.NewDiagnosticHandler(
			&log, 0, nil, uuid.Nil, uuid.Nil, nil, map[string]string{}, nil,
		),
	}, &log)
}

func TestPprofCmdlineEndpointIsBlocked(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/cmdline", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestOtherPprofEndpointsStillWork(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)

	// /debug/pprof/ index should still be served by DefaultServeMux
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
