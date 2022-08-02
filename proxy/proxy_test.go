package proxy

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	gorillaWS "github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tracing"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

var (
	testTags        = []tunnelpogs.Tag{tunnelpogs.Tag{Name: "Name", Value: "value"}}
	noWarpRouting   = ingress.WarpRoutingConfig{}
	testWarpRouting = ingress.WarpRoutingConfig{
		Enabled:        true,
		ConnectTimeout: config.CustomDuration{Duration: time.Second},
	}
)

type mockHTTPRespWriter struct {
	*httptest.ResponseRecorder
}

func newMockHTTPRespWriter() *mockHTTPRespWriter {
	return &mockHTTPRespWriter{
		httptest.NewRecorder(),
	}
}

func (w *mockHTTPRespWriter) WriteResponse() error {
	return nil
}

func (w *mockHTTPRespWriter) WriteRespHeaders(status int, header http.Header) error {
	w.WriteHeader(status)
	for header, val := range header {
		w.Header()[header] = val
	}
	return nil
}

func (w *mockHTTPRespWriter) Read(data []byte) (int, error) {
	return 0, fmt.Errorf("mockHTTPRespWriter doesn't implement io.Reader")
}

// respHeaders is a test function to read respHeaders
func (w *mockHTTPRespWriter) headers() http.Header {
	return w.Header()
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

func (w *mockWSRespWriter) Close() error {
	close(w.writeNotification)
	return nil
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
	log := zerolog.Nop()

	ctx, cancel := context.WithCancel(context.Background())

	flagSet := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	flagSet.Bool("hello-world", true, "")

	cliCtx := cli.NewContext(cli.NewApp(), flagSet, nil)
	err := cliCtx.Set("hello-world", "true")
	require.NoError(t, err)

	allowURLFromArgs := false
	ingressRule, err := ingress.NewSingleOrigin(cliCtx, allowURLFromArgs)
	require.NoError(t, err)

	require.NoError(t, ingressRule.StartOrigins(&log, ctx.Done()))

	proxy := NewOriginProxy(ingressRule, noWarpRouting, testTags, &log)
	t.Run("testProxyHTTP", testProxyHTTP(proxy))
	t.Run("testProxyWebsocket", testProxyWebsocket(proxy))
	t.Run("testProxySSE", testProxySSE(proxy))
	t.Run("testProxySSEAllData", testProxySSEAllData(proxy))
	cancel()
}

func testProxyHTTP(proxy connection.OriginProxy) func(t *testing.T) {
	return func(t *testing.T) {
		responseWriter := newMockHTTPRespWriter()
		req, err := http.NewRequest(http.MethodGet, "http://localhost:8080", nil)
		require.NoError(t, err)

		err = proxy.ProxyHTTP(responseWriter, tracing.NewTracedRequest(req), false)
		require.NoError(t, err)
		for _, tag := range testTags {
			assert.Equal(t, tag.Value, req.Header.Get(TagHeaderNamePrefix+tag.Name))
		}

		assert.Equal(t, http.StatusOK, responseWriter.Code)
	}
}

func testProxyWebsocket(proxy connection.OriginProxy) func(t *testing.T) {
	return func(t *testing.T) {
		// WSRoute is a websocket echo handler
		const testTimeout = 5 * time.Second * 1000
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		readPipe, writePipe := io.Pipe()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:8080%s", hello.WSRoute), readPipe)
		req.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		responseWriter := newMockWSRespWriter(nil)

		finished := make(chan struct{})

		errGroup, ctx := errgroup.WithContext(ctx)
		errGroup.Go(func() error {
			err = proxy.ProxyHTTP(responseWriter, tracing.NewTracedRequest(req), true)
			require.NoError(t, err)

			require.Equal(t, http.StatusSwitchingProtocols, responseWriter.Code)
			return nil
		})

		errGroup.Go(func() error {
			select {
			case <-finished:
			case <-ctx.Done():
			}
			if ctx.Err() == context.DeadlineExceeded {
				t.Errorf("Test timed out")
				readPipe.Close()
				writePipe.Close()
				responseWriter.Close()
			}
			return nil
		})

		msg := []byte("test websocket")
		err = wsutil.WriteClientText(writePipe, msg)
		require.NoError(t, err)

		// ReadServerText reads next data message from rw, considering that caller represents proxy side.
		returnedMsg, err := wsutil.ReadServerText(responseWriter.respBody())
		require.NoError(t, err)
		require.Equal(t, msg, returnedMsg)

		err = wsutil.WriteClientBinary(writePipe, msg)
		require.NoError(t, err)

		returnedMsg, err = wsutil.ReadServerBinary(responseWriter.respBody())
		require.NoError(t, err)
		require.Equal(t, msg, returnedMsg)

		_ = readPipe.Close()
		_ = writePipe.Close()
		_ = responseWriter.Close()

		close(finished)
		errGroup.Wait()
	}
}

func testProxySSE(proxy connection.OriginProxy) func(t *testing.T) {
	return func(t *testing.T) {
		var (
			pushCount = 50
			pushFreq  = time.Millisecond * 10
		)
		responseWriter := newMockSSERespWriter()
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:8080%s?freq=%s", hello.SSERoute, pushFreq), nil)
		require.NoError(t, err)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = proxy.ProxyHTTP(responseWriter, tracing.NewTracedRequest(req), false)
			require.NoError(t, err)

			require.Equal(t, http.StatusOK, responseWriter.Code)
		}()

		for i := 0; i < pushCount; i++ {
			line := responseWriter.ReadBytes()
			expect := fmt.Sprintf("%d\n", i)
			require.Equal(t, []byte(expect), line, fmt.Sprintf("Expect to read %v, got %v", expect, line))

			line = responseWriter.ReadBytes()
			require.Equal(t, []byte("\n"), line, fmt.Sprintf("Expect to read '\n', got %v", line))
		}

		cancel()
		wg.Wait()
	}
}

// Regression test to guarantee that we always write the contents downstream even if EOF is reached without
// hitting the delimiter
func testProxySSEAllData(proxy *Proxy) func(t *testing.T) {
	return func(t *testing.T) {
		eyeballReader := io.NopCloser(strings.NewReader("data\r\r"))
		responseWriter := newMockSSERespWriter()

		// responseWriter uses an unbuffered channel, so we call in a different go-routine
		go proxy.writeEventStream(responseWriter, eyeballReader)

		result := string(<-responseWriter.writeNotification)
		require.Equal(t, "data\r\r", result)
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

	tests := []MultipleIngressTest{
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

	runIngressTestScenarios(t, unvalidatedIngress, tests)
}

type MultipleIngressTest struct {
	url            string
	expectedStatus int
	expectedBody   []byte
}

func runIngressTestScenarios(t *testing.T, unvalidatedIngress []config.UnvalidatedIngressRule, tests []MultipleIngressTest) {
	ingress, err := ingress.ParseIngress(&config.Configuration{
		TunnelID: t.Name(),
		Ingress:  unvalidatedIngress,
	})
	require.NoError(t, err)

	log := zerolog.Nop()

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, ingress.StartOrigins(&log, ctx.Done()))

	proxy := NewOriginProxy(ingress, noWarpRouting, testTags, &log)

	for _, test := range tests {
		responseWriter := newMockHTTPRespWriter()
		req, err := http.NewRequest(http.MethodGet, test.url, nil)
		require.NoError(t, err)

		err = proxy.ProxyHTTP(responseWriter, tracing.NewTracedRequest(req), false)
		require.NoError(t, err)

		assert.Equal(t, test.expectedStatus, responseWriter.Code)
		if test.expectedBody != nil {
			assert.Equal(t, test.expectedBody, responseWriter.Body.Bytes())
		} else {
			assert.Equal(t, 0, responseWriter.Body.Len())
		}
	}
	cancel()
}

type mockAPI struct{}

func (ma mockAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte("Created"))
}

type errorOriginTransport struct{}

func (errorOriginTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("Proxy error")
}

func TestProxyError(t *testing.T) {
	ing := ingress.Ingress{
		Rules: []ingress.Rule{
			{
				Hostname: "*",
				Path:     nil,
				Service: ingress.MockOriginHTTPService{
					Transport: errorOriginTransport{},
				},
			},
		},
	}

	log := zerolog.Nop()

	proxy := NewOriginProxy(ing, noWarpRouting, testTags, &log)

	responseWriter := newMockHTTPRespWriter()
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1", nil)
	assert.NoError(t, err)

	assert.Error(t, proxy.ProxyHTTP(responseWriter, tracing.NewTracedRequest(req), false))
}

type replayer struct {
	sync.RWMutex
	writeDone chan struct{}
	rw        *bytes.Buffer
}

func newReplayer(buffer *bytes.Buffer) {

}

func (r *replayer) Read(p []byte) (int, error) {
	r.RLock()
	defer r.RUnlock()
	return r.rw.Read(p)
}

func (r *replayer) Write(p []byte) (int, error) {
	r.Lock()
	defer r.Unlock()
	n, err := r.rw.Write(p)
	return n, err
}

func (r *replayer) String() string {
	r.Lock()
	defer r.Unlock()
	return r.rw.String()
}

func (r *replayer) Bytes() []byte {
	r.Lock()
	defer r.Unlock()
	return r.rw.Bytes()
}

// TestConnections tests every possible permutation of connection protocols
// proxied by cloudflared.
//
// WS - WS : When a websocket based ingress is configured on the origin and
// the eyeball is also a websocket client streaming data.
// TCP - TCP : When teamnet is enabled and an http or tcp service is running
// on the origin.
// TCP - WS: When teamnet is enabled and a websocket based service is running
// on the origin.
// WS - TCP: When a tcp based ingress is configured on the origin and the
// eyeball sends tcp packets wrapped in websockets. (E.g: cloudflared access).
func TestConnections(t *testing.T) {
	logger := logger.Create(nil)
	replayer := &replayer{rw: &bytes.Buffer{}}
	type args struct {
		ingressServiceScheme  string
		originService         func(*testing.T, net.Listener)
		eyeballResponseWriter connection.ResponseWriter
		eyeballRequestBody    io.ReadCloser

		// Can be set to nil to show warp routing is not enabled.
		warpRoutingService *ingress.WarpRoutingService

		// eyeball connection type.
		connectionType connection.Type

		// requestheaders to be sent in the call to proxy.Proxy
		requestHeaders http.Header
	}

	type want struct {
		message []byte
		headers http.Header
		err     bool
	}

	var tests = []struct {
		name string
		args args
		want want
	}{
		{
			name: "ws-ws proxy",
			args: args{
				ingressServiceScheme:  "ws://",
				originService:         runEchoWSService,
				eyeballResponseWriter: newWSRespWriter(replayer),
				eyeballRequestBody:    newWSRequestBody([]byte("test1")),
				connectionType:        connection.TypeWebsocket,
				requestHeaders: map[string][]string{
					// Example key from https://tools.ietf.org/html/rfc6455#section-1.2
					"Sec-Websocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="},
					"Test-Cloudflared-Echo": {"Echo"},
				},
			},
			want: want{
				message: []byte("echo-test1"),
				headers: map[string][]string{
					"Connection":            {"Upgrade"},
					"Sec-Websocket-Accept":  {"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
					"Upgrade":               {"websocket"},
					"Test-Cloudflared-Echo": {"Echo"},
				},
			},
		},
		{
			name: "tcp-tcp proxy",
			args: args{
				ingressServiceScheme:  "tcp://",
				originService:         runEchoTCPService,
				eyeballResponseWriter: newTCPRespWriter(replayer),
				eyeballRequestBody:    newTCPRequestBody([]byte("test2")),
				warpRoutingService:    ingress.NewWarpRoutingService(testWarpRouting),
				connectionType:        connection.TypeTCP,
				requestHeaders: map[string][]string{
					"Cf-Cloudflared-Proxy-Src": {"non-blank-value"},
				},
			},
			want: want{
				message: []byte("echo-test2"),
			},
		},
		{
			name: "tcp-ws proxy",
			args: args{
				ingressServiceScheme: "ws://",
				originService:        runEchoWSService,
				// eyeballResponseWriter gets set after roundtrip dial.
				eyeballRequestBody: newPipedWSRequestBody([]byte("test3")),
				warpRoutingService: ingress.NewWarpRoutingService(testWarpRouting),
				requestHeaders: map[string][]string{
					"Cf-Cloudflared-Proxy-Src": {"non-blank-value"},
				},
				connectionType: connection.TypeTCP,
			},
			want: want{
				message: []byte("echo-test3"),
				// We expect no headers here because they are sent back via
				// the stream.
			},
		},
		{
			name: "ws-tcp proxy",
			args: args{
				ingressServiceScheme:  "tcp://",
				originService:         runEchoTCPService,
				eyeballResponseWriter: newWSRespWriter(replayer),
				eyeballRequestBody:    newWSRequestBody([]byte("test4")),
				connectionType:        connection.TypeWebsocket,
				requestHeaders: map[string][]string{
					// Example key from https://tools.ietf.org/html/rfc6455#section-1.2
					"Sec-Websocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="},
				},
			},
			want: want{
				message: []byte("echo-test4"),
				headers: map[string][]string{
					"Connection":           {"Upgrade"},
					"Sec-Websocket-Accept": {"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
					"Upgrade":              {"websocket"},
				},
			},
		},
		{
			// Send (unexpected) HTTP when origin expects WS (to unwrap for raw TCP)
			name: "http-(ws)tcp proxy",
			args: args{
				ingressServiceScheme:  "tcp://",
				originService:         runEchoTCPService,
				eyeballResponseWriter: newMockHTTPRespWriter(),
				eyeballRequestBody:    http.NoBody,
				connectionType:        connection.TypeHTTP,
				requestHeaders: map[string][]string{
					"Cf-Cloudflared-Proxy-Src": {"non-blank-value"},
				},
			},
			want: want{
				message: []byte{},
				headers: map[string][]string{},
			},
		},
		{
			name: "tcp-tcp proxy without warpRoutingService enabled",
			args: args{
				ingressServiceScheme:  "tcp://",
				originService:         runEchoTCPService,
				eyeballResponseWriter: newTCPRespWriter(replayer),
				eyeballRequestBody:    newTCPRequestBody([]byte("test2")),
				connectionType:        connection.TypeTCP,
				requestHeaders: map[string][]string{
					"Cf-Cloudflared-Proxy-Src": {"non-blank-value"},
				},
			},
			want: want{
				message: []byte{},
				err:     true,
			},
		},
		{
			name: "ws-ws proxy when origin is different",
			args: args{
				ingressServiceScheme:  "ws://",
				originService:         runEchoWSService,
				eyeballResponseWriter: newWSRespWriter(replayer),
				eyeballRequestBody:    newWSRequestBody([]byte("test1")),
				connectionType:        connection.TypeWebsocket,
				requestHeaders: map[string][]string{
					// Example key from https://tools.ietf.org/html/rfc6455#section-1.2
					"Sec-Websocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="},
					"Origin":            {"Different origin"},
				},
			},
			want: want{
				message: []byte("Forbidden\n"),
				err:     false,
				headers: map[string][]string{
					"Content-Length":         {"10"},
					"Content-Type":           {"text/plain; charset=utf-8"},
					"Sec-Websocket-Version":  {"13"},
					"X-Content-Type-Options": {"nosniff"},
				},
			},
		},
		{
			name: "tcp-* proxy when origin service has already closed the connection/ is no longer running",
			args: args{
				ingressServiceScheme: "tcp://",
				originService: func(t *testing.T, ln net.Listener) {
					// closing the listener created by the test.
					ln.Close()
				},
				eyeballResponseWriter: newTCPRespWriter(replayer),
				eyeballRequestBody:    newTCPRequestBody([]byte("test2")),
				connectionType:        connection.TypeTCP,
				requestHeaders: map[string][]string{
					"Cf-Cloudflared-Proxy-Src": {"non-blank-value"},
				},
			},
			want: want{
				message: []byte{},
				err:     true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			// Starts origin service
			test.args.originService(t, ln)

			ingressRule := createSingleIngressConfig(t, test.args.ingressServiceScheme+ln.Addr().String())
			ingressRule.StartOrigins(logger, ctx.Done())
			proxy := NewOriginProxy(ingressRule, testWarpRouting, testTags, logger)
			proxy.warpRouting = test.args.warpRoutingService

			dest := ln.Addr().String()
			req, err := http.NewRequest(
				http.MethodGet,
				test.args.ingressServiceScheme+ln.Addr().String(),
				test.args.eyeballRequestBody,
			)
			require.NoError(t, err)

			req.Header = test.args.requestHeaders
			respWriter := test.args.eyeballResponseWriter

			if pipedReqBody, ok := test.args.eyeballRequestBody.(*pipedRequestBody); ok {
				respWriter = newTCPRespWriter(pipedReqBody.pipedConn)
				go func() {
					resp := pipedReqBody.roundtrip(test.args.ingressServiceScheme + ln.Addr().String())
					replayer.Write(resp)
				}()
			}
			if test.args.connectionType == connection.TypeTCP {
				rwa := connection.NewHTTPResponseReadWriterAcker(respWriter, req)
				err = proxy.ProxyTCP(ctx, rwa, &connection.TCPRequest{Dest: dest})
			} else {
				err = proxy.ProxyHTTP(respWriter, tracing.NewTracedRequest(req), test.args.connectionType == connection.TypeWebsocket)
			}

			cancel()
			assert.Equal(t, test.want.err, err != nil)
			assert.Equal(t, test.want.message, replayer.Bytes())
			respPrinter := respWriter.(responsePrinter)
			assert.Equal(t, test.want.headers, respPrinter.headers())
			replayer.rw.Reset()
		})
	}
}

type requestBody struct {
	pw *io.PipeWriter
	pr *io.PipeReader
}

func newWSRequestBody(data []byte) *requestBody {
	pr, pw := io.Pipe()
	go wsutil.WriteClientBinary(pw, data)
	return &requestBody{
		pr: pr,
		pw: pw,
	}
}
func newTCPRequestBody(data []byte) *requestBody {
	pr, pw := io.Pipe()
	go pw.Write(data)
	return &requestBody{
		pr: pr,
		pw: pw,
	}
}

func (r *requestBody) Read(p []byte) (n int, err error) {
	return r.pr.Read(p)
}

func (r *requestBody) Close() error {
	r.pw.Close()
	r.pr.Close()
	return nil
}

type pipedRequestBody struct {
	dialer         gorillaWS.Dialer
	pipedConn      net.Conn
	wsConn         net.Conn
	messageToWrite []byte
}

func newPipedWSRequestBody(data []byte) *pipedRequestBody {
	conn1, conn2 := net.Pipe()
	dialer := gorillaWS.Dialer{
		NetDial: func(network, addr string) (net.Conn, error) {
			return conn2, nil
		},
	}
	return &pipedRequestBody{
		dialer:         dialer,
		pipedConn:      conn1,
		wsConn:         conn2,
		messageToWrite: data,
	}
}

func (p *pipedRequestBody) roundtrip(addr string) []byte {
	header := http.Header{}
	conn, resp, err := p.dialer.Dial(addr, header)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		panic(fmt.Errorf("resp returned status code: %d", resp.StatusCode))
	}

	err = conn.WriteMessage(gorillaWS.TextMessage, p.messageToWrite)
	if err != nil {
		panic(err)
	}

	_, data, err := conn.ReadMessage()
	if err != nil {
		panic(err)
	}

	return data
}

func (p *pipedRequestBody) Read(data []byte) (n int, err error) {
	return p.pipedConn.Read(data)
}

func (p *pipedRequestBody) Close() error {
	return nil
}

type responsePrinter interface {
	headers() http.Header
}

type wsRespWriter struct {
	w               io.Writer
	responseHeaders http.Header
	code            int
}

// newWSRespWriter uses wsutil.WriteClientText to generate websocket frames.
// and wsutil.ReadClientText to translate frames from server to byte data.
// In essence, this acts as a wsClient.
func newWSRespWriter(w io.Writer) *wsRespWriter {
	return &wsRespWriter{
		w: w,
	}
}

// Write is written to by ingress.Stream and serves as the output to the client.
func (w *wsRespWriter) Write(p []byte) (int, error) {
	returnedMsg, err := wsutil.ReadServerBinary(bytes.NewBuffer(p))
	if err != nil {
		// The data was not returned by a websocket connection.
		if err != io.ErrUnexpectedEOF {
			return w.w.Write(p)
		}
	}
	return w.w.Write(returnedMsg)
}

func (w *wsRespWriter) WriteRespHeaders(status int, header http.Header) error {
	w.responseHeaders = header
	w.code = status
	return nil
}

// respHeaders is a test function to read respHeaders
func (w *wsRespWriter) headers() http.Header {
	// Removing indeterminstic header because it cannot be asserted.
	w.responseHeaders.Del("Date")
	return w.responseHeaders
}

type mockTCPRespWriter struct {
	w               io.Writer
	responseHeaders http.Header
	code            int
}

func newTCPRespWriter(w io.Writer) *mockTCPRespWriter {
	return &mockTCPRespWriter{
		w: w,
	}
}

func (m *mockTCPRespWriter) Read(p []byte) (n int, err error) {
	return len(p), nil
}

func (m *mockTCPRespWriter) Write(p []byte) (n int, err error) {
	return m.w.Write(p)
}

func (m *mockTCPRespWriter) WriteRespHeaders(status int, header http.Header) error {
	m.responseHeaders = header
	m.code = status
	return nil
}

// respHeaders is a test function to read respHeaders
func (m *mockTCPRespWriter) headers() http.Header {
	return m.responseHeaders
}

func createSingleIngressConfig(t *testing.T, service string) ingress.Ingress {
	ingressConfig := &config.Configuration{
		Ingress: []config.UnvalidatedIngressRule{
			{
				Hostname: "*",
				Service:  service,
			},
		},
	}
	ingressRule, err := ingress.ParseIngress(ingressConfig)
	require.NoError(t, err)
	return ingressRule
}

func runEchoTCPService(t *testing.T, l net.Listener) {
	go func() {
		for {
			conn, err := l.Accept()
			require.NoError(t, err)
			defer conn.Close()

			for {
				buf := make([]byte, 1024)
				size, err := conn.Read(buf)
				if err == io.EOF {
					return
				}
				data := []byte("echo-")
				data = append(data, buf[:size]...)
				_, err = conn.Write(data)
				if err != nil {
					t.Log(err)
				}
				return
			}
		}
	}()
}

func runEchoWSService(t *testing.T, l net.Listener) {
	var upgrader = gorillaWS.Upgrader{
		ReadBufferSize:  10,
		WriteBufferSize: 10,
	}

	var ws = func(w http.ResponseWriter, r *http.Request) {
		header := make(http.Header)
		for k, vs := range r.Header {
			if k == "Test-Cloudflared-Echo" {
				header[k] = vs
			}
		}
		conn, err := upgrader.Upgrade(w, r, header)
		if err != nil {
			t.Log(err)
			return
		}
		defer conn.Close()

		for {
			messageType, p, err := conn.ReadMessage()
			if err != nil {
				return
			}
			data := []byte("echo-")
			data = append(data, p...)
			if err := conn.WriteMessage(messageType, data); err != nil {
				return
			}
		}
	}

	server := http.Server{
		Handler: http.HandlerFunc(ws),
	}

	go func() {
		err := server.Serve(l)
		require.NoError(t, err)
	}()
}
