package origin

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tlsconfig"

	"github.com/gobwas/ws/wsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func (w *mockHTTPRespWriter) WriteErrorResponse(err error) {
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

func TestProxy(t *testing.T) {
	logger, err := logger.New()
	require.NoError(t, err)
	// let runtime pick an available port
	listener, err := hello.CreateTLSListener("127.0.0.1:0")
	require.NoError(t, err)

	originURL := &url.URL{
		Scheme: "https",
		Host:   listener.Addr().String(),
	}
	originCA := x509.NewCertPool()
	helloCert, err := tlsconfig.GetHelloCertificateX509()
	require.NoError(t, err)
	originCA.AddCert(helloCert)
	clientTLS := &tls.Config{
		RootCAs: originCA,
	}
	proxyConfig := &ProxyConfig{
		Client: &http.Transport{
			TLSClientConfig: clientTLS,
		},
		URL:       originURL,
		TLSConfig: clientTLS,
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		hello.StartHelloWorldServer(logger, listener, ctx.Done())
	}()

	client := NewClient(proxyConfig, logger)
	t.Run("testProxyHTTP", testProxyHTTP(t, client, originURL))
	t.Run("testProxyWebsocket", testProxyWebsocket(t, client, originURL, clientTLS))
	t.Run("testProxySSE", testProxySSE(t, client, originURL))
	cancel()
}

func testProxyHTTP(t *testing.T, client connection.OriginClient, originURL *url.URL) func(t *testing.T) {
	return func(t *testing.T) {
		respWriter := newMockHTTPRespWriter()
		req, err := http.NewRequest(http.MethodGet, originURL.String(), nil)
		require.NoError(t, err)

		err = client.Proxy(respWriter, req, false)
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, respWriter.Code)
	}
}

func testProxyWebsocket(t *testing.T, client connection.OriginClient, originURL *url.URL, tlsConfig *tls.Config) func(t *testing.T) {
	return func(t *testing.T) {
		// WSRoute is a websocket echo handler
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s%s", originURL, hello.WSRoute), nil)

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

func testProxySSE(t *testing.T, client connection.OriginClient, originURL *url.URL) func(t *testing.T) {
	return func(t *testing.T) {
		var (
			pushCount = 50
			pushFreq  = time.Duration(time.Millisecond * 10)
		)
		respWriter := newMockSSERespWriter()
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s%s?freq=%s", originURL, hello.SSERoute, pushFreq), nil)
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
