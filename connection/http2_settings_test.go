package connection

import (
	"context"
	"io"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func TestHTTP2MaxConcurrentStreams(t *testing.T) {
	// Currently, the only value set for the http2.Server is MaxConcurrentStreams
	// this test should be updated in case there is an addition to the values that
	// are set.

	t.Parallel()
	conn, edgeConn := newTestHTTP2Connection()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = conn.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = edgeConn.Close()
		select {
		case <-served:
		case <-time.After(5 * time.Second):
			t.Fatal("HTTP/2 connection did not shut down")
		}
	})
	deadline, _ := ctx.Deadline()
	require.NoError(t, edgeConn.SetDeadline(deadline))

	// net.Pipe is unbuffered, so read SETTINGS while the server reads the preface.
	prefaceWritten := make(chan error, 1)
	go func() {
		_, err := io.WriteString(edgeConn, http2.ClientPreface)
		prefaceWritten <- err
	}()
	frame, err := http2.NewFramer(edgeConn, edgeConn).ReadFrame()
	require.NoError(t, err)
	settings, ok := frame.(*http2.SettingsFrame)
	require.True(t, ok, "first server frame must be SETTINGS")
	limit, ok := settings.Value(http2.SettingMaxConcurrentStreams)
	require.True(t, ok, "SETTINGS_MAX_CONCURRENT_STREAMS must be advertised")
	require.Equal(t, uint32(math.MaxInt32), limit)
	require.NoError(t, <-prefaceWritten)
}
