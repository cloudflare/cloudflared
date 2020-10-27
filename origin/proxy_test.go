package origin

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/logger"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/urfave/cli/v2"

	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testTags = []tunnelpogs.Tag(nil)
)

type mockHTTPRespWriter struct {
	*httptest.ResponseRecorder
}

func newMockHTTPRespWriter() *mockHTTPRespWriter {
	return &mockHTTPRespWriter{
		httptest.NewRecorder(),
	}
}

func (w *mockHTTPRespWriter) WriteRespHeaders(resp *http.Response) error {
	w.WriteHeader(resp.StatusCode)
	for header, val := range resp.Header {
		w.Header()[header] = val
	}
	return nil
}

func (w *mockHTTPRespWriter) WriteErrorResponse() {
	w.WriteHeader(http.StatusBadGateway)
}

func (w *mockHTTPRespWriter) Read(data []byte) (int, error) {
	return 0, fmt.Errorf("mockHTTPRespWriter doesn't implement io.Reader")
}

type mockWSRespWriter struct {
	*mockHTTPRespWriter
	writeNotification chan []byte
	reader            io.Reader
}

func newMockWSRespWriter(reader io.Reader) *mockWSRespWriter {
	return &mockWSRespWriter{
		newMockHTTPRespWriter(),
		make(chan []byte),
		reader,
	}
}

func (w *mockWSRespWriter) Write(data []byte) (int, error) {
	w.writeNotification <- data
	return len(data), nil
}

func (w *mockWSRespWriter) respBody() io.ReadWriter {
	data := <-w.writeNotification
	return bytes.NewBuffer(data)
}

func (w *mockWSRespWriter) Read(data []byte) (int, error) {
	return w.reader.Read(data)
}

type mockSSERespWriter struct {
	*mockHTTPRespWriter
	writeNotification chan []byte
}

func newMockSSERespWriter() *mockSSERespWriter {
	return &mockSSERespWriter{
		newMockHTTPRespWriter(),
		make(chan []byte),
	}
}

func (w *mockSSERespWriter) Write(data []byte) (int, error) {
	w.writeNotification <- data
	return len(data), nil
}

func (w *mockSSERespWriter) ReadBytes() []byte {
	return <-w.writeNotification
}

func TestProxySingleOrigin(t *testing.T) {
	logger, err := logger.New()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	flagSet := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	flagSet.Bool("hello-world", true, "")

	cliCtx := cli.NewContext(cli.NewApp(), flagSet, nil)
	err = cliCtx.Set("hello-world", "true")
	require.NoError(t, err)

	allowURLFromArgs := false
	ingressRule, err := ingress.NewSingleOrigin(cliCtx, allowURLFromArgs, logger)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errC := make(chan error)
	ingressRule.StartOrigins(&wg, logger, ctx.Done(), errC)

	client := NewClient(ingressRule, testTags, logger)
	t.Run("testProxyHTTP", testProxyHTTP(t, client))
	t.Run("testProxyWebsocket", testProxyWebsocket(t, client))
	t.Run("testProxySSE", testProxySSE(t, client))
	cancel()
	wg.Wait()
}

func testProxyHTTP(t *testing.T, client connection.OriginClient) func(t *testing.T) {
	return func(t *testing.T) {
		respWriter := newMockHTTPRespWriter()
		req, err := http.NewRequest(http.MethodGet, "http://localhost:8080", nil)
		require.NoError(t, err)

		err = client.Proxy(respWriter, req, false)
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, respWriter.Code)
	}
}

func testProxyWebsocket(t *testing.T, client connection.OriginClient) func(t *testing.T) {
	return func(t *testing.T) {
		// WSRoute is a websocket echo handler
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:8080%s", hello.WSRoute), nil)

		readPipe, writePipe := io.Pipe()
		respWriter := newMockWSRespWriter(readPipe)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = client.Proxy(respWriter, req, true)
			require.NoError(t, err)

			require.Equal(t, http.StatusSwitchingProtocols, respWriter.Code)
		}()

		msg := []byte("test websocket")
		err = wsutil.WriteClientText(writePipe, msg)
		require.NoError(t, err)

		// ReadServerText reads next data message from rw, considering that caller represents client side.
		returnedMsg, err := wsutil.ReadServerText(respWriter.respBody())
		require.NoError(t, err)
		require.Equal(t, msg, returnedMsg)

		err = wsutil.WriteClientBinary(writePipe, msg)
		require.NoError(t, err)

		returnedMsg, err = wsutil.ReadServerBinary(respWriter.respBody())
		require.NoError(t, err)
		require.Equal(t, msg, returnedMsg)

		cancel()
		wg.Wait()
	}
}

func testProxySSE(t *testing.T, client connection.OriginClient) func(t *testing.T) {
	return func(t *testing.T) {
		var (
			pushCount = 50
			pushFreq  = time.Duration(time.Millisecond * 10)
		)
		respWriter := newMockSSERespWriter()
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:8080%s?freq=%s", hello.SSERoute, pushFreq), nil)
		require.NoError(t, err)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = client.Proxy(respWriter, req, false)
			require.NoError(t, err)

			require.Equal(t, http.StatusOK, respWriter.Code)
		}()

		for i := 0; i < pushCount; i++ {
			line := respWriter.ReadBytes()
			expect := fmt.Sprintf("%d\n", i)
			require.Equal(t, []byte(expect), line, fmt.Sprintf("Expect to read %v, got %v", expect, line))

			line = respWriter.ReadBytes()
			require.Equal(t, []byte("\n"), line, fmt.Sprintf("Expect to read '\n', got %v", line))
		}

		cancel()
		wg.Wait()
	}
}

func TestProxyMultipleOrigins(t *testing.T) {
	api := httptest.NewServer(mockAPI{})
	defer api.Close()

	unvalidatedIngress := []config.UnvalidatedIngressRule{
		{
			Hostname: "api.example.com",
			Service:  api.URL,
		},
		{
			Hostname: "hello.example.com",
			Service:  "hello-world",
		},
		{
			Hostname: "health.example.com",
			Path:     "/health",
			Service:  "http_status:200",
		},
		{
			Hostname: "*",
			Service:  "http_status:404",
		},
	}

	ingress, err := ingress.ParseIngress(&config.Configuration{
		TunnelID: t.Name(),
		Ingress:  unvalidatedIngress,
	})
	require.NoError(t, err)

	logger, err := logger.New()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error)
	var wg sync.WaitGroup
	ingress.StartOrigins(&wg, logger, ctx.Done(), errC)

	client := NewClient(ingress, testTags, logger)

	tests := []struct {
		url            string
		expectedStatus int
		expectedBody   []byte
	}{
		{
			url:            "http://api.example.com",
			expectedStatus: http.StatusCreated,
			expectedBody:   []byte("Created"),
		},
		{
			url:            fmt.Sprintf("http://hello.example.com%s", hello.HealthRoute),
			expectedStatus: http.StatusOK,
			expectedBody:   []byte("ok"),
		},
		{
			url:            "http://health.example.com/health",
			expectedStatus: http.StatusOK,
		},
		{
			url:            "http://health.example.com/",
			expectedStatus: http.StatusNotFound,
		},
		{
			url:            "http://not-found.example.com",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, test := range tests {
		respWriter := newMockHTTPRespWriter()
		req, err := http.NewRequest(http.MethodGet, test.url, nil)
		require.NoError(t, err)

		err = client.Proxy(respWriter, req, false)
		require.NoError(t, err)

		assert.Equal(t, test.expectedStatus, respWriter.Code)
		if test.expectedBody != nil {
			assert.Equal(t, test.expectedBody, respWriter.Body.Bytes())
		} else {
			assert.Equal(t, 0, respWriter.Body.Len())
		}
	}
	cancel()
	wg.Wait()
}

type mockAPI struct{}

func (ma mockAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte("Created"))
}
