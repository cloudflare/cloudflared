package ingress

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// A symlink inside the root that points at a dotfile is only caught by the
// resolved-path check, so this pins that step.
func TestFileServerRejectsSymlinkToDotfile(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, ".env"), "SECRET=1")
	if err := os.Symlink(filepath.Join(root, ".env"), filepath.Join(root, "link_env")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	svc := newTestFileServer(t, root)
	if rec := serve(svc, "GET", "/link_env"); rec.Code != 404 {
		t.Errorf("symlink to dotfile: code=%d want 404", rec.Code)
	}
}

// The loopback listener must only answer requests that came through the tunnel.
// This fails if RoundTrip stops injecting the header, which no other test covers.
func TestFileServerLoopbackRequiresTunnelRoundTrip(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.txt"), "AAA")
	svc := newTestFileServer(t, root)

	shutdown := make(chan struct{})
	defer close(shutdown)
	logger := zerolog.Nop()
	require.NoError(t, svc.start(&logger, shutdown, OriginRequestConfig{}))

	// Through the tunnel proxy: RoundTrip adds the header.
	client := &http.Client{Transport: svc}
	resp, err := client.Get("http://example.com/a.txt")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "AAA", string(body))

	// Straight at the loopback port, the way another local process would.
	direct, err := http.Get("http://" + svc.httpService.url.Host + "/a.txt")
	require.NoError(t, err)
	defer direct.Body.Close()
	require.Equal(t, http.StatusForbidden, direct.StatusCode)
}
