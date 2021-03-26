package connection

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/h2mux"
)

var (
	testMuxerConfig = &MuxerConfig{
		HeartbeatInterval:  time.Second * 5,
		MaxHeartbeats:      5,
		CompressionSetting: 0,
		MetricsUpdateFreq:  time.Second * 5,
	}
)

func newH2MuxConnection(t require.TestingT) (*h2muxConnection, *h2mux.Muxer) {
	edgeConn, originConn := net.Pipe()
	edgeMuxChan := make(chan *h2mux.Muxer)
	go func() {
		edgeMuxConfig := h2mux.MuxerConfig{
			Log: &log,
			Handler: h2mux.MuxedStreamFunc(func(stream *h2mux.MuxedStream) error {
				// we only expect RPC traffic in client->edge direction, provide minimal support for mocking
				require.True(t, stream.IsRPCStream())
				return stream.WriteHeaders([]h2mux.Header{
					{Name: ":status", Value: "200"},
				})
			}),
		}
		edgeMux, err := h2mux.Handshake(edgeConn, edgeConn, edgeMuxConfig, h2mux.ActiveStreams)
		require.NoError(t, err)
		edgeMuxChan <- edgeMux
	}()
	var connIndex = uint8(0)
	testObserver := NewObserver(&log, &log, false)
	h2muxConn, err, _ := NewH2muxConnection(testConfig, testMuxerConfig, originConn, connIndex, testObserver, nil)
	require.NoError(t, err)
	return h2muxConn, <-edgeMuxChan
}

func TestServeStreamHTTP(t *testing.T) {
	tests := []testRequest{
		{
			name:           "ok",
			endpoint:       "/ok",
			expectedStatus: http.StatusOK,
			expectedBody:   []byte(http.StatusText(http.StatusOK)),
		},
		{
			name:           "large_file",
			endpoint:       "/large_file",
			expectedStatus: http.StatusOK,
			expectedBody:   testLargeResp,
		},
		{
			name:           "Bad request",
			endpoint:       "/400",
			expectedStatus: http.StatusBadRequest,
			expectedBody:   []byte(http.StatusText(http.StatusBadRequest)),
		},
		{
			name:           "Internal server error",
			endpoint:       "/500",
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   []byte(http.StatusText(http.StatusInternalServerError)),
		},
		{
			name:           "Proxy error",
			endpoint:       "/error",
			expectedStatus: http.StatusBadGateway,
			expectedBody:   nil,
			isProxyError:   true,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	h2muxConn, edgeMux := newH2MuxConnection(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = edgeMux.Serve(ctx)
	}()
	go func() {
		defer wg.Done()
		err := h2muxConn.serveMuxer(ctx)
		require.Error(t, err)
	}()

	for _, test := range tests {
		headers := []h2mux.Header{
			{
				Name:  ":path",
				Value: test.endpoint,
			},
		}
		stream, err := edgeMux.OpenStream(ctx, headers, nil)
		require.NoError(t, err)
		require.True(t, hasHeader(stream, ":status", strconv.Itoa(test.expectedStatus)))

		if test.isProxyError {
			assert.True(t, hasHeader(stream, ResponseMetaHeader, responseMetaHeaderCfd))
		} else {
			assert.True(t, hasHeader(stream, ResponseMetaHeader, responseMetaHeaderOrigin))
			body := make([]byte, len(test.expectedBody))
			_, err = stream.Read(body)
			require.NoError(t, err)
			require.Equal(t, test.expectedBody, body)
		}
	}
	cancel()
	wg.Wait()
}

func TestServeStreamWS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h2muxConn, edgeMux := newH2MuxConnection(t)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		edgeMux.Serve(ctx)
	}()
	go func() {
		defer wg.Done()
		err := h2muxConn.serveMuxer(ctx)
		require.Error(t, err)
	}()

	headers := []h2mux.Header{
		{
			Name:  ":path",
			Value: "/ws",
		},
		{
			Name:  "connection",
			Value: "upgrade",
		},
		{
			Name:  "upgrade",
			Value: "websocket",
		},
	}

	readPipe, writePipe := io.Pipe()
	stream, err := edgeMux.OpenStream(ctx, headers, readPipe)
	require.NoError(t, err)

	require.True(t, hasHeader(stream, ":status", strconv.Itoa(http.StatusSwitchingProtocols)))
	assert.True(t, hasHeader(stream, ResponseMetaHeader, responseMetaHeaderOrigin))

	data := []byte("test websocket")
	err = wsutil.WriteClientText(writePipe, data)
	require.NoError(t, err)

	respBody, err := wsutil.ReadServerText(stream)
	require.NoError(t, err)
	require.Equal(t, data, respBody, fmt.Sprintf("Expect %s, got %s", string(data), string(respBody)))

	cancel()
	wg.Wait()
}

func TestGracefulShutdownH2Mux(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h2muxConn, edgeMux := newH2MuxConnection(t)

	shutdownC := make(chan struct{})
	unregisteredC := make(chan struct{})
	h2muxConn.gracefulShutdownC = shutdownC
	h2muxConn.newRPCClientFunc = func(_ context.Context, _ io.ReadWriteCloser, _ *zerolog.Logger) NamedTunnelRPCClient {
		return &mockNamedTunnelRPCClient{
			registered:   nil,
			unregistered: unregisteredC,
		}
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		_ = edgeMux.Serve(ctx)
	}()
	go func() {
		defer wg.Done()
		_ = h2muxConn.serveMuxer(ctx)
	}()

	go func() {
		defer wg.Done()
		h2muxConn.controlLoop(ctx, &mockConnectedFuse{}, true)
	}()

	time.Sleep(100 * time.Millisecond)
	close(shutdownC)

	select {
	case <-unregisteredC:
		break // ok
	case <-time.Tick(time.Second):
		assert.Fail(t, "timed out waiting for control loop to unregister")
	}

	cancel()
	wg.Wait()

	assert.True(t, h2muxConn.stoppedGracefully)
	assert.Nil(t, h2muxConn.gracefulShutdownC)
}

func hasHeader(stream *h2mux.MuxedStream, name, val string) bool {
	for _, header := range stream.Headers {
		if header.Name == name && header.Value == val {
			return true
		}
	}
	return false
}

func benchmarkServeStreamHTTPSimple(b *testing.B, test testRequest) {
	ctx, cancel := context.WithCancel(context.Background())
	h2muxConn, edgeMux := newH2MuxConnection(b)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		edgeMux.Serve(ctx)
	}()
	go func() {
		defer wg.Done()
		err := h2muxConn.serveMuxer(ctx)
		require.Error(b, err)
	}()

	headers := []h2mux.Header{
		{
			Name:  ":path",
			Value: test.endpoint,
		},
	}

	body := make([]byte, len(test.expectedBody))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StartTimer()
		stream, openstreamErr := edgeMux.OpenStream(ctx, headers, nil)
		_, readBodyErr := stream.Read(body)
		b.StopTimer()

		require.NoError(b, openstreamErr)
		assert.True(b, hasHeader(stream, ResponseMetaHeader, responseMetaHeaderOrigin))
		require.True(b, hasHeader(stream, ":status", strconv.Itoa(http.StatusOK)))
		require.NoError(b, readBodyErr)
		require.Equal(b, test.expectedBody, body)
	}

	cancel()
	wg.Wait()
}

func BenchmarkServeStreamHTTPSimple(b *testing.B) {
	test := testRequest{
		name:           "ok",
		endpoint:       "/ok",
		expectedStatus: http.StatusOK,
		expectedBody:   []byte(http.StatusText(http.StatusOK)),
	}

	benchmarkServeStreamHTTPSimple(b, test)
}

func BenchmarkServeStreamHTTPLargeFile(b *testing.B) {
	test := testRequest{
		name:           "large_file",
		endpoint:       "/large_file",
		expectedStatus: http.StatusOK,
		expectedBody:   testLargeResp,
	}

	benchmarkServeStreamHTTPSimple(b, test)
}
