//go:build !windows

package ingress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/config"
)

func newRetryUnixOrigin(t *testing.T, timeout time.Duration, scheme string) *unixSocketPath {
	t.Helper()
	// Keeping the socket pathname below the Unix sockaddr limit on macOS as well as Linux.
	dir, err := os.MkdirTemp("", "cfd-retry-") //nolint:usetesting // t.TempDir includes the test name and can exceed sockaddr_un limits.
	require.NoError(t, err)
	t.Cleanup(func() { err := os.RemoveAll(dir); assert.NoError(t, err) })
	origin := &unixSocketPath{path: filepath.Join(dir, "http.sock"), scheme: scheme}
	cfg := originRequestFromConfig(config.OriginRequestConfig{})
	cfg.ConnectRetryTimeout.Duration = timeout
	cfg.NoTLSVerify = true
	err = origin.start(TestLogger, t.Context().Done(), cfg)
	require.NoError(t, err)
	t.Cleanup(origin.transport.CloseIdleConnections)
	return origin
}

func startRetryUnixServer(t *testing.T, origin *unixSocketPath, handler http.Handler, http2 bool) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("unix", origin.path)
	require.NoError(t, err)
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}, EnableHTTP2: http2}
	if origin.scheme == "https" {
		server.StartTLS()
	} else {
		server.Start()
	}
	t.Cleanup(server.Close)
	return server
}

func TestUnixOriginWaitsForListener(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"http", "https", "http2", "stale socket"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			scheme := "http"
			if mode == "https" || mode == "http2" {
				scheme = "https"
			}
			origin := newRetryUnixOrigin(t, 2*time.Second, scheme)
			origin.transport.ForceAttemptHTTP2 = mode == "http2"
			if mode == "stale socket" {
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: origin.path, Net: "unix"})
				require.NoError(t, err)
				listener.SetUnlinkOnClose(false)
				err = listener.Close()
				require.NoError(t, err)
			}
			const arrivals = 8
			failedDial := make(chan struct{}, arrivals)
			results := make(chan error, arrivals)
			var reads atomic.Int32
			var received atomic.Int32
			for i := range arrivals {
				go func() {
					var once sync.Once
					trace := &httptrace.ClientTrace{ConnectDone: func(_, _ string, err error) {
						if err != nil {
							once.Do(func() { failedDial <- struct{}{} })
						}
					}}
					ctx := httptrace.WithClientTrace(t.Context(), trace)
					// A streaming, non-replayable POST body: GetBody remains nil.
					body := &countingRetryBody{Reader: strings.NewReader(fmt.Sprintf("payload-%d", i)), reads: &reads}
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/", body)
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
					data, err := io.ReadAll(resp.Body)
					if err == nil && string(data) != fmt.Sprintf("payload-%d", i) {
						err = fmt.Errorf("incorrect body: %q", data)
					}
					results <- err
				}()
			}
			for range arrivals {
				select {
				case <-failedDial:
				case <-time.After(5 * time.Second):
					t.Fatal("request never attempted its connection")
				}
			}
			require.Zero(t, reads.Load(), "request bodies must remain unread while the listener is absent")
			if mode == "stale socket" {
				err := os.Remove(origin.path)
				require.NoError(t, err)
			}
			startRetryUnixServer(t, origin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				if mode == "http2" {
					assert.Equal(t, 2, r.ProtoMajor)
				}
				_, err := io.Copy(w, r.Body)
				assert.NoError(t, err)
			}), mode == "http2")
			for range arrivals {
				select {
				case err := <-results:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("request did not resume after the listener appeared")
				}
			}
			require.EqualValues(t, arrivals, received.Load(), "each POST must arrive exactly once")
		})
	}
}

type countingRetryBody struct {
	io.Reader
	reads *atomic.Int32
}

func (b *countingRetryBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	return b.Reader.Read(p)
}

func TestUnixOriginRetryDisabledAndExpired(t *testing.T) {
	t.Parallel()
	for _, timeout := range []time.Duration{0, 50 * time.Millisecond} {
		name := "disabled"
		if timeout > 0 {
			name = "retry window exhausted"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			origin := newRetryUnixOrigin(t, timeout, "http")
			var attempts atomic.Int32
			trace := &httptrace.ClientTrace{ConnectStart: func(_, _ string) { attempts.Add(1) }}
			ctx := httptrace.WithClientTrace(t.Context(), trace)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/", nil)
			require.NoError(t, err)
			resp, err := origin.RoundTrip(req)
			if resp != nil {
				defer func() { _ = resp.Body.Close() }()
			}
			require.Error(t, err)
			require.Nil(t, resp)
			if timeout == 0 {
				require.ErrorIs(t, err, os.ErrNotExist)
				require.NotErrorIs(t, err, context.DeadlineExceeded)
				require.EqualValues(t, 1, attempts.Load())
				return
			}
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Greater(t, attempts.Load(), int32(1))
		})
	}
}

func TestUnixOriginRetryStopsWhenRequestEnds(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name    string
		timeout time.Duration
		err     error
	}{
		{"request canceled", time.Minute, context.Canceled},
		{"request deadline exceeded", 250 * time.Millisecond, context.DeadlineExceeded},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			origin := newRetryUnixOrigin(t, time.Minute, "http")
			dial := origin.transport.DialContext
			dialStopped := make(chan struct{})
			origin.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				defer close(dialStopped)
				return dial(ctx, network, address)
			}
			failed := make(chan struct{})
			var once sync.Once
			trace := &httptrace.ClientTrace{ConnectDone: func(_, _ string, err error) {
				if err != nil {
					once.Do(func() { close(failed) })
				}
			}}
			ctx, cancel := context.WithTimeout(t.Context(), tc.timeout)
			defer cancel()
			req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, "http://localhost/", nil)
			require.NoError(t, err)
			result := make(chan error, 1)
			go func() {
				resp, err := origin.RoundTrip(req)
				if resp != nil {
					_ = resp.Body.Close()
				}
				result <- err
			}()
			select {
			case <-failed:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not encounter the absent listener")
			}
			if tc.err == context.Canceled {
				cancel()
			}
			select {
			case err := <-result:
				require.ErrorIs(t, err, tc.err)
			case <-time.After(time.Second):
				t.Fatal("request ignored cancellation")
			}
			select {
			case <-dialStopped:
			case <-time.After(time.Second):
				t.Fatal("dial kept retrying after request ended")
			}
		})
	}
}

func TestUnixOriginRetriesAfterPooledConnectionCloses(t *testing.T) {
	t.Parallel()
	origin := newRetryUnixOrigin(t, 2*time.Second, "http")
	old := startRetryUnixServer(t, origin, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(w, "old")
		assert.NoError(t, err)
	}), false)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/", nil)
	require.NoError(t, err)
	resp, err := origin.RoundTrip(req)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	err = resp.Body.Close()
	require.NoError(t, err)
	old.Close()
	failed := make(chan struct{})
	var once sync.Once
	trace := &httptrace.ClientTrace{ConnectDone: func(_, _ string, err error) {
		if err != nil {
			once.Do(func() { close(failed) })
		}
	}}
	req, err = http.NewRequestWithContext(httptrace.WithClientTrace(t.Context(), trace), http.MethodGet, "http://localhost/", nil)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		resp, err := origin.RoundTrip(req)
		if err != nil {
			result <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		if err == nil && string(data) != "new" {
			err = fmt.Errorf("incorrect release: %q", data)
		}
		result <- err
	}()
	select {
	case <-failed:
	case <-time.After(5 * time.Second):
		t.Fatal("no connection attempted during restart")
	}
	startRetryUnixServer(t, origin, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(w, "new")
		assert.NoError(t, err)
	}), false)
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach new instance")
	}
}

func TestUnixOriginDoesNotReplayDeliveredPOST(t *testing.T) {
	t.Parallel()
	origin := newRetryUnixOrigin(t, time.Second, "http")
	var delivered atomic.Int32
	startRetryUnixServer(t, origin, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		assert.NoError(t, err)
		delivered.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if !assert.NoError(t, err) {
			return
		}
		err = conn.Close()
		assert.NoError(t, err)
	}), false)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost/", strings.NewReader("operation"))
	require.NoError(t, err)
	resp, err := origin.RoundTrip(req)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	require.Error(t, err)
	require.Nil(t, resp)
	require.EqualValues(t, 1, delivered.Load())
}

func TestUnixOriginRetryDoesNotCancelResponseBody(t *testing.T) {
	t.Parallel()
	origin := newRetryUnixOrigin(t, time.Second, "http")
	release := make(chan struct{})
	defer close(release)
	startRetryUnixServer(t, origin, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
		_, err := io.WriteString(w, "streamed response")
		assert.NoError(t, err)
	}), false)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/", nil)
	require.NoError(t, err)
	resp, err := origin.RoundTrip(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	release <- struct{}{}
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "streamed response", string(data))
}
