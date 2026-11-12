package ingress

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/config"
)

func TestHTTPOriginWaitsForListener(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	err = listener.Close()
	require.NoError(t, err)
	originURL, err := url.Parse("http://" + address)
	require.NoError(t, err)
	origin := &httpService{url: originURL}
	cfg := originRequestFromConfig(config.OriginRequestConfig{})
	cfg.ConnectRetryTimeout.Duration = 2 * time.Second
	err = origin.start(TestLogger, t.Context().Done(), cfg)
	require.NoError(t, err)
	t.Cleanup(origin.transport.CloseIdleConnections)
	failed := make(chan struct{})
	var once sync.Once
	trace := &httptrace.ClientTrace{ConnectDone: func(_, _ string, err error) {
		if err != nil {
			once.Do(func() { close(failed) })
		}
	}}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(t.Context(), trace), http.MethodGet, "http://localhost/", nil)
	require.NoError(t, err)
	results := make(chan error, 1)
	go func() {
		resp, err := origin.RoundTrip(req)
		if err != nil {
			results <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		assert.Equal(t, "origin ready", string(body))
		results <- err
	}()
	select {
	case <-failed:
	case <-time.After(5 * time.Second):
		t.Fatal("no refused connection")
	}
	listener, err = net.Listen("tcp", address)
	require.NoError(t, err)
	server := &httptest.Server{Listener: listener, Config: &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(w, "origin ready")
		assert.NoError(t, err)
	})}}
	server.Start()
	defer server.Close()
	select {
	case err := <-results:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("origin connection did not recover")
	}
}

func TestHTTPOriginRetryDoesNotRetryTLSOrHTTPFailures(t *testing.T) {
	t.Parallel()
	for _, tlsFailure := range []bool{false, true} {
		name := "HTTP 503"
		if tlsFailure {
			name = "invalid certificate"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var received atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				received.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
				_, err := io.WriteString(w, "temporarily unavailable")
				assert.NoError(t, err)
			}))
			if tlsFailure {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			originURL, err := url.Parse(server.URL)
			require.NoError(t, err)
			origin := &httpService{url: originURL}
			cfg := originRequestFromConfig(config.OriginRequestConfig{})
			cfg.ConnectRetryTimeout.Duration = time.Second
			err = origin.start(TestLogger, t.Context().Done(), cfg)
			require.NoError(t, err)
			defer origin.transport.CloseIdleConnections()
			var attempts atomic.Int32
			trace := &httptrace.ClientTrace{ConnectStart: func(_, _ string) { attempts.Add(1) }}
			req, err := http.NewRequestWithContext(httptrace.WithClientTrace(t.Context(), trace), http.MethodGet, "http://localhost/", nil)
			require.NoError(t, err)
			resp, err := origin.RoundTrip(req)
			if tlsFailure {
				var certErr *tls.CertificateVerificationError
				require.ErrorAs(t, err, &certErr)
				require.Nil(t, resp)
				require.Zero(t, received.Load())
			} else {
				require.NoError(t, err)
				defer func() { _ = resp.Body.Close() }()
				require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Equal(t, "temporarily unavailable", string(body))
				require.EqualValues(t, 1, received.Load())
			}
			require.EqualValues(t, 1, attempts.Load())
		})
	}
}

func TestHTTPOriginRetryStopsUnusedDial(t *testing.T) {
	t.Parallel()
	firstArrived := make(chan struct{})
	releaseFirst := make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(releaseFirst) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			close(firstArrived)
			<-releaseFirst
		}
		_, err := io.WriteString(w, "ok")
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	originURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	origin := &httpService{url: originURL}
	cfg := originRequestFromConfig(config.OriginRequestConfig{})
	cfg.ConnectRetryTimeout.Duration = time.Minute
	err = origin.start(TestLogger, t.Context().Done(), cfg)
	require.NoError(t, err)
	defer origin.transport.CloseIdleConnections()
	dial := origin.transport.DialContext
	stopped := make(chan struct{})
	origin.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			close(stopped)
		}
		return conn, err
	}
	failed := make(chan struct{})
	var once sync.Once
	trace := &httptrace.ClientTrace{ConnectDone: func(_, _ string, err error) {
		if err != nil {
			once.Do(func() { close(failed) })
		}
	}}
	results := make(chan error, 2)
	send := func(path string) {
		req, err := http.NewRequestWithContext(httptrace.WithClientTrace(t.Context(), trace), http.MethodGet, "http://localhost"+path, nil)
		if err != nil {
			results <- err
			return
		}
		resp, err := origin.RoundTrip(req)
		if err != nil {
			results <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		assert.Equal(t, "ok", string(body))
		results <- err
	}
	go send("/first")
	select {
	case <-firstArrived:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not arrive")
	}
	// Closing only the listener keeps the first connection available for reuse.
	err = server.Listener.Close()
	require.NoError(t, err)
	go send("/second")
	select {
	case <-failed:
	case <-time.After(5 * time.Second):
		t.Fatal("second request did not encounter the absent listener")
	}
	release.Do(func() { close(releaseFirst) })
	for range 2 {
		select {
		case err := <-results:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("request did not finish")
		}
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("unused dial outlived its request")
	}
}
