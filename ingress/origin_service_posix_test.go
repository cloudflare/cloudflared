//go:build !windows

package ingress

import (
	"errors"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestUnixSocketH2COrigin(t *testing.T) {
	t.Parallel()

	socketFile, err := os.CreateTemp("/tmp", "cloudflared-h2c-*.sock")
	require.NoError(t, err)
	socketPath := socketFile.Name()
	require.NoError(t, socketFile.Close())
	require.NoError(t, os.Remove(socketPath))

	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := os.Remove(socketPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("failed to remove Unix socket: %v", err)
		}
	})

	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Origin-Protocol", r.Proto)
		}),
		Protocols: protocols,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		require.NoError(t, server.Close())
		require.ErrorIs(t, <-serveErr, http.ErrServerClosed)
	})

	log := zerolog.Nop()
	service := &unixSocketPath{
		path:   socketPath,
		scheme: "http",
	}
	require.NoError(t, service.start(&log, nil, OriginRequestConfig{Http2Origin: true}))
	t.Cleanup(service.transport.CloseIdleConnections)

	req, err := http.NewRequest(http.MethodGet, "http://localhost", nil)
	require.NoError(t, err)
	resp, err := service.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, "HTTP/2.0", resp.Header.Get("X-Origin-Protocol"))
	require.Equal(t, 2, resp.ProtoMajor)
	require.NoError(t, resp.Body.Close())
}
