package origin

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/logger"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/ingress"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	gorillaWS "github.com/gorilla/websocket"
	"github.com/urfave/cli/v2"

	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testTags                 = []tunnelpogs.Tag(nil)
	unusedWarpRoutingService = (*ingress.WarpRoutingService)(nil)
)

type mockHTTPRespWriter struct {
	*httptest.ResponseRecorder
}

func newMockHTTPRespWriter() *mockHTTPRespWriter {
	return &mockHTTPRespWriter{
		httptest.NewRecorder(),
	}
}

func (w *mockHTTPRespWriter) WriteRespHeaders(status int, header http.Header) error {
	w.WriteHeader(status)
	for header, val := range header {
		w.Header()[header] = val
	}
	return nil
}

func (w *mockHTTPRespWriter) WriteErrorResponse() {
	w.WriteHeader(http.StatusBadGateway)
	_, _ = w.Write([]byte("http response error"))
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

	var wg sync.WaitGroup
	errC := make(chan error)
	require.NoError(t, ingressRule.StartOrigins(&wg, &log, ctx.Done(), errC))

	proxy := NewOriginProxy(ingressRule, unusedWarpRoutingService, testTags, &log)
	t.Run("testProxyHTTP", testProxyHTTP(t, proxy))
	t.Run("testProxyWebsocket", testProxyWebsocket(t, proxy))
	t.Run("testProxySSE", testProxySSE(t, proxy))
	cancel()
	wg.Wait()
}

func testProxyHTTP(t *testing.T, proxy connection.OriginProxy) func(t *testing.T) {
	return func(t *testing.T) {
		respWriter := newMockHTTPRespWriter()
		req, err := http.NewRequest(http.MethodGet, "http://localhost:8080", nil)
		require.NoError(t, err)

		err = proxy.Proxy(respWriter, req, connection.TypeHTTP)
		require.NoError(t, err)

		assert.Equal(t, http.StatusOK, respWriter.Code)
	}
}

func testProxyWebsocket(t *testing.T, proxy connection.OriginProxy) func(t *testing.T) {
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
			err = proxy.Proxy(respWriter, req, connection.TypeWebsocket)
			require.NoError(t, err)

			require.Equal(t, http.StatusSwitchingProtocols, respWriter.Code)
		}()

		msg := []byte("test websocket")
		err = wsutil.WriteClientText(writePipe, msg)
		require.NoError(t, err)

		// ReadServerText reads next data message from rw, considering that caller represents proxy side.
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

func testProxySSE(t *testing.T, proxy connection.OriginProxy) func(t *testing.T) {
	return func(t *testing.T) {
		var (
			pushCount = 50
			pushFreq  = time.Millisecond * 10
		)
		respWriter := newMockSSERespWriter()
		ctx, cancel := context.WithCancel(context.Background())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:8080%s?freq=%s", hello.SSERoute, pushFreq), nil)
		require.NoError(t, err)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = proxy.Proxy(respWriter, req, connection.TypeHTTP)
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

	log := zerolog.Nop()

	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error)
	var wg sync.WaitGroup
	require.NoError(t, ingress.StartOrigins(&wg, &log, ctx.Done(), errC))

	proxy := NewOriginProxy(ingress, unusedWarpRoutingService, testTags, &log)

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

		err = proxy.Proxy(respWriter, req, connection.TypeHTTP)
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
	_, _ = w.Write([]byte("Created"))
}

type errorOriginTransport struct{}

func (errorOriginTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("Proxy error")
}

func TestProxyError(t *testing.T) {
	ingress := ingress.Ingress{
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

	proxy := NewOriginProxy(ingress, unusedWarpRoutingService, testTags, &log)

	respWriter := newMockHTTPRespWriter()
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1", nil)
	assert.NoError(t, err)

	assert.Error(t, proxy.Proxy(respWriter, req, connection.TypeHTTP))
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

	var tests = []struct {
		name                 string
		skip                 bool
		ingressServicePrefix string

		originService  func(*testing.T, net.Listener)
		eyeballService connection.ResponseWriter
		connectionType connection.Type
		requestHeaders http.Header
		wantMessage    []byte
		wantHeaders    http.Header
	}{
		{
			name:                 "ws-ws proxy",
			ingressServicePrefix: "ws://",
			originService:        runEchoWSService,
			eyeballService:       newWSRespWriter([]byte("test1"), replayer),
			connectionType:       connection.TypeWebsocket,
			requestHeaders: http.Header{
				// Example key from https://tools.ietf.org/html/rfc6455#section-1.2
				"Sec-Websocket-Key":     {"dGhlIHNhbXBsZSBub25jZQ=="},
				"Test-Cloudflared-Echo": {"Echo"},
			},
			wantMessage: []byte("echo-test1"),
			wantHeaders: http.Header{
				"Connection":            {"Upgrade"},
				"Sec-Websocket-Accept":  {"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
				"Upgrade":               {"websocket"},
				"Test-Cloudflared-Echo": {"Echo"},
			},
		},
		{
			name:                 "tcp-tcp proxy",
			ingressServicePrefix: "tcp://",
			originService:        runEchoTCPService,
			eyeballService: newTCPRespWriter(
				[]byte(`test2`),
				replayer,
			),
			connectionType: connection.TypeTCP,
			requestHeaders: http.Header{
				"Cf-Cloudflared-Proxy-Src": {"non-blank-value"},
			},
			wantMessage: []byte("echo-test2"),
		},
		{
			name:                 "tcp-ws proxy",
			ingressServicePrefix: "ws://",
			originService:        runEchoWSService,
			eyeballService:       newPipedWSWriter(&mockTCPRespWriter{}, []byte("test3")),
			requestHeaders: http.Header{
				"Cf-Cloudflared-Proxy-Src": {"non-blank-value"},
			},
			connectionType: connection.TypeTCP,
			wantMessage:    []byte("echo-test3"),
			// We expect no headers here because they are sent back via
			// the stream.
		},
		{
			name:                 "ws-tcp proxy",
			ingressServicePrefix: "tcp://",
			originService:        runEchoTCPService,
			eyeballService:       newWSRespWriter([]byte("test4"), replayer),
			connectionType:       connection.TypeWebsocket,
			requestHeaders: http.Header{
				// Example key from https://tools.ietf.org/html/rfc6455#section-1.2
				"Sec-Websocket-Key": {"dGhlIHNhbXBsZSBub25jZQ=="},
			},
			wantMessage: []byte("echo-test4"),
			wantHeaders: http.Header{
				"Connection":           {"Upgrade"},
				"Sec-Websocket-Accept": {"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
				"Upgrade":              {"websocket"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			// Starts origin service
			test.originService(t, ln)

			ingressRule := createSingleIngressConfig(t, test.ingressServicePrefix+ln.Addr().String())
			var wg sync.WaitGroup
			errC := make(chan error)
			ingressRule.StartOrigins(&wg, logger, ctx.Done(), errC)
			proxy := NewOriginProxy(ingressRule, ingress.NewWarpRoutingService(), testTags, logger)

			req, err := http.NewRequest(http.MethodGet, test.ingressServicePrefix+ln.Addr().String(), nil)
			require.NoError(t, err)
			req.Header = test.requestHeaders

			if pipedWS, ok := test.eyeballService.(*pipedWSWriter); ok {
				go func() {
					resp := pipedWS.roundtrip(test.ingressServicePrefix + ln.Addr().String())
					replayer.Write(resp)
				}()
			}

			err = proxy.Proxy(test.eyeballService, req, test.connectionType)
			require.NoError(t, err)

			cancel()
			assert.Equal(t, test.wantMessage, replayer.Bytes())
			respPrinter := test.eyeballService.(responsePrinter)
			assert.Equal(t, test.wantHeaders, respPrinter.printRespHeaders())
			replayer.rw.Reset()
		})
	}
}

type responsePrinter interface {
	printRespHeaders() http.Header
}

type pipedWSWriter struct {
	dialer         gorillaWS.Dialer
	wsConn         net.Conn
	pipedConn      net.Conn
	respWriter     connection.ResponseWriter
	respHeaders    http.Header
	messageToWrite []byte
}

func newPipedWSWriter(rw *mockTCPRespWriter, messageToWrite []byte) *pipedWSWriter {
	conn1, conn2 := net.Pipe()
	dialer := gorillaWS.Dialer{
		NetDial: func(network, addr string) (net.Conn, error) {
			return conn2, nil
		},
	}
	rw.pr = conn1
	rw.w = conn1
	return &pipedWSWriter{
		dialer:         dialer,
		pipedConn:      conn1,
		wsConn:         conn2,
		messageToWrite: messageToWrite,
		respWriter:     rw,
	}
}

func (p *pipedWSWriter) roundtrip(addr string) []byte {
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

func (p *pipedWSWriter) Read(data []byte) (int, error) {
	return p.pipedConn.Read(data)
}

func (p *pipedWSWriter) Write(data []byte) (int, error) {
	return p.pipedConn.Write(data)
}

func (p *pipedWSWriter) WriteErrorResponse() {
}

func (p *pipedWSWriter) WriteRespHeaders(status int, header http.Header) error {
	p.respHeaders = header
	return nil
}

// printRespHeaders is a test function to read respHeaders
func (p *pipedWSWriter) printRespHeaders() http.Header {
	return p.respHeaders
}

type wsRespWriter struct {
	w           io.Writer
	pr          *io.PipeReader
	pw          *io.PipeWriter
	respHeaders http.Header
	code        int
}

// newWSRespWriter uses wsutil.WriteClientText to generate websocket frames.
// and wsutil.ReadClientText to translate frames from server to byte data.
// In essence, this acts as a wsClient.
func newWSRespWriter(data []byte, w io.Writer) *wsRespWriter {
	pr, pw := io.Pipe()
	go wsutil.WriteClientBinary(pw, data)
	return &wsRespWriter{
		w:  w,
		pr: pr,
		pw: pw,
	}
}

// Read is read by ingress.Stream and serves as the input from the client.
func (w *wsRespWriter) Read(p []byte) (int, error) {
	return w.pr.Read(p)
}

// Write is written to by ingress.Stream and serves as the output to the client.
func (w *wsRespWriter) Write(p []byte) (int, error) {
	defer w.pw.Close()
	returnedMsg, err := wsutil.ReadServerBinary(bytes.NewBuffer(p))
	if err != nil {
		// The data was not returned by a websocket connecton.
		if err != io.ErrUnexpectedEOF {
			return w.w.Write(p)
		}
	}
	return w.w.Write(returnedMsg)
}

func (w *wsRespWriter) WriteRespHeaders(status int, header http.Header) error {
	w.respHeaders = header
	w.code = status
	return nil
}

func (w *wsRespWriter) WriteErrorResponse() {
}

// printRespHeaders is a test function to read respHeaders
func (w *wsRespWriter) printRespHeaders() http.Header {
	return w.respHeaders
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
					return
				}
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
		require.NoError(t, err)
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

type tcpWrappedWs struct {
}

type mockTCPRespWriter struct {
	w           io.Writer
	pr          io.Reader
	pw          *io.PipeWriter
	respHeaders http.Header
	code        int
}

func newTCPRespWriter(data []byte, w io.Writer) *mockTCPRespWriter {
	pr, pw := io.Pipe()
	go pw.Write(data)
	return &mockTCPRespWriter{
		w:  w,
		pr: pr,
		pw: pw,
	}
}

func (m *mockTCPRespWriter) Read(p []byte) (n int, err error) {
	return m.pr.Read(p)
}

func (m *mockTCPRespWriter) Write(p []byte) (n int, err error) {
	defer m.pw.Close()
	return m.w.Write(p)
}

func (m *mockTCPRespWriter) WriteErrorResponse() {
}

func (m *mockTCPRespWriter) WriteRespHeaders(status int, header http.Header) error {
	m.respHeaders = header
	m.code = status
	return nil
}

// printRespHeaders is a test function to read respHeaders
func (m *mockTCPRespWriter) printRespHeaders() http.Header {
	return m.respHeaders
}
