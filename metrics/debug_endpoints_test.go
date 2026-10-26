package metrics

import (
	"encoding/json"
	"expvar"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "golang.org/x/net/trace" // ensure /debug/requests and /debug/events are registered on http.DefaultServeMux

	"github.com/cloudflare/cloudflared/diagnostic"
)

// cmdlineMarker identifies os.Args in a response body. The base name of the test
// binary is used rather than the full path because the path may contain
// characters that are escaped in JSON responses (e.g. backslashes on Windows).
var cmdlineMarker = filepath.Base(os.Args[0])

func testHandler(t *testing.T) *http.ServeMux {
	t.Helper()

	log := zerolog.Nop()
	return newMetricsHandler(Config{
		DiagnosticHandler: diagnostic.NewDiagnosticHandler(
			&log, 0, nil, uuid.Nil, uuid.Nil, nil, map[string]string{}, nil,
		),
	}, &log)
}

// TestDefaultServeMuxLeaksCmdline documents why http.DefaultServeMux must never be
// mounted on the metrics server: handlers registered on it by imported packages
// expose os.Args, which may contain secrets such as tunnel tokens.
func TestDefaultServeMuxLeaksCmdline(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/debug/pprof/cmdline", "/debug/vars"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		http.DefaultServeMux.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "path %s", path)
		require.Contains(t, w.Body.String(), cmdlineMarker, "path %s", path)
	}
}

func TestAllowlistedDebugEndpointsAreRegistered(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	for path, pattern := range map[string]string{
		"/debug/pprof/":          "/debug/pprof/",
		"/debug/pprof/heap":      "/debug/pprof/",
		"/debug/pprof/goroutine": "/debug/pprof/",
		"/debug/pprof/profile":   "/debug/pprof/profile",
		"/debug/pprof/symbol":    "/debug/pprof/symbol",
		"/debug/pprof/trace":     "/debug/pprof/trace",
		"/debug/vars":            "GET /debug/vars",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		_, got := handler.Handler(req)
		assert.Equal(t, pattern, got, "path %s", path)
	}
}

func TestAllowlistedDebugEndpointsServe(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	for _, path := range []string{
		"/debug/pprof/",
		"/debug/pprof/heap",
		"/debug/pprof/goroutine",
		"/debug/pprof/allocs",
		"/debug/pprof/symbol",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "path %s", path)
	}
}

func TestNonAllowlistedDebugEndpointsAreNotServed(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	for _, path := range []string{
		"/debug/",
		"/debug/pprof/cmdline",
		"/debug/requests",
		"/debug/events",
		// Path variants that http.ServeMux normalizes.
		"/debug/vars/",
		"//debug/vars",
		"/debug/./vars",
		"/debug/pprof/../vars",
		"/debug/pprof/cmdline/",
		"/debug//pprof/cmdline",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			req := httptest.NewRequest(method, path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.NotEqual(t, http.StatusOK, w.Code, "%s %s", method, path)
			assert.NotContains(t, w.Body.String(), cmdlineMarker, "%s %s", method, path)
		}
	}
}

func TestExpvarEndpointServesOnlyAllowlistedVars(t *testing.T) {
	t.Parallel()

	// Sanity check: the default expvar set includes the sensitive "cmdline" var.
	require.NotNil(t, expvar.Get("cmdline"))

	handler := testHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/debug/vars", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.NotContains(t, w.Body.String(), cmdlineMarker)

	var vars map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &vars))
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	assert.ElementsMatch(t, allowedExpvars, keys)
	assert.NotContains(t, vars, "cmdline")

	var memstats map[string]any
	require.NoError(t, json.Unmarshal(vars["memstats"], &memstats))
	assert.Contains(t, memstats, "HeapAlloc")
}

func TestExpvarEndpointRejectsNonGetMethods(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/debug/vars", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code, "method %s", method)
		assert.NotContains(t, w.Body.String(), cmdlineMarker, "method %s", method)
	}
}

func TestPprofCmdlineEndpointIsNotFound(t *testing.T) {
	t.Parallel()

	handler := testHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/cmdline", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
