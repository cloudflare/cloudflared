package updater

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testStallTimeout is a short download deadline so the tests do not wait for the production clientTimeout.
const testStallTimeout = 500 * time.Millisecond

// TestDownloadSlowBodyExceedsClientTimeout checks that a download whose body takes longer overall than
// the stall timeout still completes, as long as data keeps arriving, as it does on slow connections.
// The body spans about 750ms with gaps of a quarter of the timeout, so it runs for real time.
func TestDownloadSlowBodyExceedsClientTimeout(t *testing.T) {
	t.Parallel()

	chunk := []byte("0123456789")
	const chunks = 6
	gap := testStallTimeout / 4
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < chunks; i++ {
			_, _ = w.Write(chunk)
			flusher.Flush()
			time.Sleep(gap)
		}
	}))
	defer ts.Close()

	target := filepath.Join(t.TempDir(), "cloudflared.new")
	require.NoError(t, download(ts.URL+"/download", target, false, testStallTimeout))

	dat, err := os.ReadFile(target)
	require.NoError(t, err)
	require.True(t, bytes.Equal(bytes.Repeat(chunk, chunks), dat), "downloaded body does not match what the server sent")
}

// TestDownloadStalledBodyFails checks that a download which stops receiving data is aborted after
// the stall timeout with the stall error instead of hanging.
func TestDownloadStalledBodyFails(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-block
	}))
	defer ts.Close()
	defer close(block)

	target := filepath.Join(t.TempDir(), "cloudflared.new")
	err := download(ts.URL+"/download", target, false, testStallTimeout)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stalled")
}

// TestDownloadNoResponseFails checks that a server which sends no response headers is aborted after
// the stall timeout with the stall error instead of hanging.
func TestDownloadNoResponseFails(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer ts.Close()
	defer close(block)

	target := filepath.Join(t.TempDir(), "cloudflared.new")
	err := download(ts.URL+"/download", target, false, testStallTimeout)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stalled")
}
