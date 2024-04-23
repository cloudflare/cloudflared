package connection

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/datagramsession"
	quicpogs "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/tracing"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

var (
	testTLSServerConfig = quicpogs.GenerateTLSConfig()
	testQUICConfig      = &quic.Config{
		KeepAlivePeriod: 5 * time.Second,
		EnableDatagrams: true,
	}
	defaultQUICTimeout = 30 * time.Second
)

var _ ReadWriteAcker = (*streamReadWriteAcker)(nil)

// TestQUICServer tests if a quic server accepts and responds to a quic client with the acceptance protocol.
// It also serves as a demonstration for communication with the QUIC connection started by a cloudflared.
func TestQUICServer(t *testing.T) {
	// This is simply a sample websocket frame message.
	wsBuf := &bytes.Buffer{}
	wsutil.WriteClientBinary(wsBuf, []byte("Hello"))

	var tests = []struct {
		desc             string
		dest             string
		connectionType   quicpogs.ConnectionType
		metadata         []quicpogs.Metadata
		message          []byte
		expectedResponse []byte
	}{
		{
			desc:           "test http proxy",
			dest:           "/ok",
			connectionType: quicpogs.ConnectionTypeHTTP,
			metadata: []quicpogs.Metadata{
				{
					Key: "HttpHeader:Cf-Ray",
					Val: "123123123",
				},
				{
					Key: "HttpHost",
					Val: "cf.host",
				},
				{
					Key: "HttpMethod",
					Val: "GET",
				},
			},
			expectedResponse: []byte("OK"),
		},
		{
			desc:           "test http body request streaming",
			dest:           "/slow_echo_body",
			connectionType: quicpogs.ConnectionTypeHTTP,
			metadata: []quicpogs.Metadata{
				{
					Key: "HttpHeader:Cf-Ray",
					Val: "123123123",
				},
				{
					Key: "HttpHost",
					Val: "cf.host",
				},
				{
					Key: "HttpMethod",
					Val: "POST",
				},
				{
					Key: "HttpHeader:Content-Length",
					Val: "24",
				},
			},
			message:          []byte("This is the message body"),
			expectedResponse: []byte("This is the message body"),
		},
		{
			desc:           "test ws proxy",
			dest:           "/ws/echo",
			connectionType: quicpogs.ConnectionTypeWebsocket,
			metadata: []quicpogs.Metadata{
				{
					Key: "HttpHeader:Cf-Cloudflared-Proxy-Connection-Upgrade",
					Val: "Websocket",
				},
				{
					Key: "HttpHeader:Another-Header",
					Val: "Misc",
				},
				{
					Key: "HttpHost",
					Val: "cf.host",
				},
				{
					Key: "HttpMethod",
					Val: "get",
				},
			},
			message:          wsBuf.Bytes(),
			expectedResponse: []byte{0x82, 0x5, 0x48, 0x65, 0x6c, 0x6c, 0x6f},
		},
		{
			desc:             "test tcp proxy",
			connectionType:   quicpogs.ConnectionTypeTCP,
			metadata:         []quicpogs.Metadata{},
			message:          []byte("Here is some tcp data"),
			expectedResponse: []byte("Here is some tcp data"),
		},
	}

	for i, test := range tests {
		test := test // capture range variable
		t.Run(test.desc, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			// Start a UDP Listener for QUIC.
			udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
			require.NoError(t, err)
			udpListener, err := net.ListenUDP(udpAddr.Network(), udpAddr)
			require.NoError(t, err)
			defer udpListener.Close()
			quicTransport := &quic.Transport{Conn: udpListener, ConnectionIDLength: 16}
			quicListener, err := quicTransport.Listen(testTLSServerConfig, testQUICConfig)
			require.NoError(t, err)

			serverDone := make(chan struct{})
			go func() {
				quicServer(
					ctx, t, quicListener, test.dest, test.connectionType, test.metadata, test.message, test.expectedResponse,
				)
				close(serverDone)
			}()

			qc := testQUICConnection(udpListener.LocalAddr(), t, uint8(i))

			connDone := make(chan struct{})
			go func() {
				qc.Serve(ctx)
				close(connDone)
			}()

			<-serverDone
			cancel()
			<-connDone
		})
	}
}

type fakeControlStream struct {
	ControlStreamHandler
}

func (fakeControlStream) ServeControlStream(ctx context.Context, rw io.ReadWriteCloser, connOptions *tunnelpogs.ConnectionOptions, tunnelConfigGetter TunnelConfigJSONGetter) error {
	<-ctx.Done()
	return nil
}
func (fakeControlStream) IsStopped() bool {
	return true
}

func quicServer(
	ctx context.Context,
	t *testing.T,
	listener *quic.Listener,
	dest string,
	connectionType quicpogs.ConnectionType,
	metadata []quicpogs.Metadata,
	message []byte,
	expectedResponse []byte,
) {
	session, err := listener.Accept(ctx)
	require.NoError(t, err)

	quicStream, err := session.OpenStreamSync(context.Background())
	require.NoError(t, err)
	stream := quicpogs.NewSafeStreamCloser(quicStream, defaultQUICTimeout, &log)

	reqClientStream := quicpogs.RequestClientStream{ReadWriteCloser: stream}
	err = reqClientStream.WriteConnectRequestData(dest, connectionType, metadata...)
	require.NoError(t, err)

	_, err = reqClientStream.ReadConnectResponseData()
	require.NoError(t, err)

	if message != nil {
		// ALPN successful. Write data.
		_, err := stream.Write(message)
		require.NoError(t, err)
	}

	response := make([]byte, len(expectedResponse))
	_, err = stream.Read(response)
	if err != io.EOF {
		require.NoError(t, err)
	}

	// For now it is an echo server. Verify if the same data is returned.
	assert.Equal(t, expectedResponse, response)
}

type mockOriginProxyWithRequest struct{}

func (moc *mockOriginProxyWithRequest) ProxyHTTP(w ResponseWriter, tr *tracing.TracedHTTPRequest, isWebsocket bool) error {
	// These are a series of crude tests to ensure the headers and http related data is transferred from
	// metadata.
	r := tr.Request
	if r.Method == "" {
		return errors.New("method not sent")
	}
	if r.Host == "" {
		return errors.New("host not sent")
	}
	if len(r.Header) == 0 {
		return errors.New("headers not set")
	}

	if isWebsocket {
		return wsEchoEndpoint(w, r)
	}
	switch r.URL.Path {
	case "/ok":
		originRespEndpoint(w, http.StatusOK, []byte(http.StatusText(http.StatusOK)))
	case "/slow_echo_body":
		time.Sleep(5)
		fallthrough
	case "/echo_body":
		resp := &http.Response{
			StatusCode: http.StatusOK,
		}
		_ = w.WriteRespHeaders(resp.StatusCode, resp.Header)
		io.Copy(w, r.Body)
	case "/error":
		return fmt.Errorf("Failed to proxy to origin")
	default:
		originRespEndpoint(w, http.StatusNotFound, []byte("page not found"))
	}
	return nil
}

func TestBuildHTTPRequest(t *testing.T) {
	var tests = []struct {
		name           string
		connectRequest *quicpogs.ConnectRequest
		body           io.ReadCloser
		req            *http.Request
	}{
		{
			name: "check if http.Request is built correctly with content length",
			connectRequest: &quicpogs.ConnectRequest{
				Dest: "http://test.com",
				Metadata: []quicpogs.Metadata{
					{
						Key: "HttpHeader:Cf-Cloudflared-Proxy-Connection-Upgrade",
						Val: "Websocket",
					},
					{
						Key: "HttpHeader:Content-Length",
						Val: "514",
					},
					{
						Key: "HttpHeader:Another-Header",
						Val: "Misc",
					},
					{
						Key: "HttpHost",
						Val: "cf.host",
					},
					{
						Key: "HttpMethod",
						Val: "get",
					},
				},
			},
			req: &http.Request{
				Method: "get",
				URL: &url.URL{
					Scheme: "http",
					Host:   "test.com",
				},
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					"Another-Header": []string{"Misc"},
					"Content-Length": []string{"514"},
				},
				ContentLength: 514,
				Host:          "cf.host",
				Body:          io.NopCloser(&bytes.Buffer{}),
			},
			body: io.NopCloser(&bytes.Buffer{}),
		},
		{
			name: "if content length isn't part of request headers, then it's not set",
			connectRequest: &quicpogs.ConnectRequest{
				Dest: "http://test.com",
				Metadata: []quicpogs.Metadata{
					{
						Key: "HttpHeader:Cf-Cloudflared-Proxy-Connection-Upgrade",
						Val: "Websocket",
					},
					{
						Key: "HttpHeader:Another-Header",
						Val: "Misc",
					},
					{
						Key: "HttpHost",
						Val: "cf.host",
					},
					{
						Key: "HttpMethod",
						Val: "get",
					},
				},
			},
			req: &http.Request{
				Method: "get",
				URL: &url.URL{
					Scheme: "http",
					Host:   "test.com",
				},
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					"Another-Header": []string{"Misc"},
				},
				ContentLength: 0,
				Host:          "cf.host",
				Body:          http.NoBody,
			},
			body: io.NopCloser(&bytes.Buffer{}),
		},
		{
			name: "if content length is 0, but transfer-encoding is chunked, body is not nil",
			connectRequest: &quicpogs.ConnectRequest{
				Dest: "http://test.com",
				Metadata: []quicpogs.Metadata{
					{
						Key: "HttpHeader:Another-Header",
						Val: "Misc",
					},
					{
						Key: "HttpHeader:Transfer-Encoding",
						Val: "chunked",
					},
					{
						Key: "HttpHost",
						Val: "cf.host",
					},
					{
						Key: "HttpMethod",
						Val: "get",
					},
				},
			},
			req: &http.Request{
				Method: "get",
				URL: &url.URL{
					Scheme: "http",
					Host:   "test.com",
				},
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					"Another-Header":    []string{"Misc"},
					"Transfer-Encoding": []string{"chunked"},
				},
				ContentLength: 0,
				Host:          "cf.host",
				Body:          io.NopCloser(&bytes.Buffer{}),
			},
			body: io.NopCloser(&bytes.Buffer{}),
		},
		{
			name: "if content length is 0, but transfer-encoding is gzip,chunked, body is not nil",
			connectRequest: &quicpogs.ConnectRequest{
				Dest: "http://test.com",
				Metadata: []quicpogs.Metadata{
					{
						Key: "HttpHeader:Another-Header",
						Val: "Misc",
					},
					{
						Key: "HttpHeader:Transfer-Encoding",
						Val: "gzip,chunked",
					},
					{
						Key: "HttpHost",
						Val: "cf.host",
					},
					{
						Key: "HttpMethod",
						Val: "get",
					},
				},
			},
			req: &http.Request{
				Method: "get",
				URL: &url.URL{
					Scheme: "http",
					Host:   "test.com",
				},
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					"Another-Header":    []string{"Misc"},
					"Transfer-Encoding": []string{"gzip,chunked"},
				},
				ContentLength: 0,
				Host:          "cf.host",
				Body:          io.NopCloser(&bytes.Buffer{}),
			},
			body: io.NopCloser(&bytes.Buffer{}),
		},
		{
			name: "if content length is 0, and connect request is a websocket, body is not nil",
			connectRequest: &quicpogs.ConnectRequest{
				Type: quicpogs.ConnectionTypeWebsocket,
				Dest: "http://test.com",
				Metadata: []quicpogs.Metadata{
					{
						Key: "HttpHeader:Another-Header",
						Val: "Misc",
					},
					{
						Key: "HttpHost",
						Val: "cf.host",
					},
					{
						Key: "HttpMethod",
						Val: "get",
					},
				},
			},
			req: &http.Request{
				Method: "get",
				URL: &url.URL{
					Scheme: "http",
					Host:   "test.com",
				},
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header: http.Header{
					"Another-Header": []string{"Misc"},
				},
				ContentLength: 0,
				Host:          "cf.host",
				Body:          io.NopCloser(&bytes.Buffer{}),
			},
			body: io.NopCloser(&bytes.Buffer{}),
		},
	}

	log := zerolog.Nop()
	for _, test := range tests {
		test := test // capture range variable
		t.Run(test.name, func(t *testing.T) {
			req, err := buildHTTPRequest(context.Background(), test.connectRequest, test.body, 0, &log)
			assert.NoError(t, err)
			test.req = test.req.WithContext(req.Context())
			assert.Equal(t, test.req, req.Request)
		})
	}
}

func (moc *mockOriginProxyWithRequest) ProxyTCP(ctx context.Context, rwa ReadWriteAcker, tcpRequest *TCPRequest) error {
	rwa.AckConnection("")
	io.Copy(rwa, rwa)
	return nil
}

func TestServeUDPSession(t *testing.T) {
	// Start a UDP Listener for QUIC.
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	udpListener, err := net.ListenUDP(udpAddr.Network(), udpAddr)
	require.NoError(t, err)
	defer udpListener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	val := udpListener.LocalAddr()

	// Establish QUIC connection with edge
	edgeQUICSessionChan := make(chan quic.Connection)
	go func() {
		earlyListener, err := quic.Listen(udpListener, testTLSServerConfig, testQUICConfig)
		require.NoError(t, err)

		edgeQUICSession, err := earlyListener.Accept(ctx)
		require.NoError(t, err)
		edgeQUICSessionChan <- edgeQUICSession
	}()

	// Random index to avoid reusing port
	qc := testQUICConnection(val, t, 28)
	go qc.Serve(ctx)

	edgeQUICSession := <-edgeQUICSessionChan
	serveSession(ctx, qc, edgeQUICSession, closedByOrigin, io.EOF.Error(), t)
	serveSession(ctx, qc, edgeQUICSession, closedByTimeout, datagramsession.SessionIdleErr(time.Millisecond*50).Error(), t)
	serveSession(ctx, qc, edgeQUICSession, closedByRemote, "eyeball closed connection", t)
	cancel()
}

func TestNopCloserReadWriterCloseBeforeEOF(t *testing.T) {
	readerWriter := nopCloserReadWriter{ReadWriteCloser: &mockReaderNoopWriter{Reader: strings.NewReader("123456789")}}
	buffer := make([]byte, 5)

	n, err := readerWriter.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, n, 5)

	// close
	require.NoError(t, readerWriter.Close())

	// read should get error
	n, err = readerWriter.Read(buffer)
	require.Equal(t, n, 0)
	require.Equal(t, err, fmt.Errorf("closed by handler"))
}

func TestNopCloserReadWriterCloseAfterEOF(t *testing.T) {
	readerWriter := nopCloserReadWriter{ReadWriteCloser: &mockReaderNoopWriter{Reader: strings.NewReader("123456789")}}
	buffer := make([]byte, 20)

	n, err := readerWriter.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, n, 9)

	// force another read to read eof
	_, err = readerWriter.Read(buffer)
	require.Equal(t, err, io.EOF)

	// close
	require.NoError(t, readerWriter.Close())

	// read should get EOF still
	n, err = readerWriter.Read(buffer)
	require.Equal(t, n, 0)
	require.Equal(t, err, io.EOF)
}

func TestCreateUDPConnReuseSourcePort(t *testing.T) {
	logger := zerolog.Nop()
	conn, err := createUDPConnForConnIndex(0, nil, &logger)
	require.NoError(t, err)

	getPortFunc := func(conn *net.UDPConn) int {
		addr := conn.LocalAddr().(*net.UDPAddr)
		return addr.Port
	}

	initialPort := getPortFunc(conn)

	// close conn
	conn.Close()

	// should get the same port as before.
	conn, err = createUDPConnForConnIndex(0, nil, &logger)
	require.NoError(t, err)
	require.Equal(t, initialPort, getPortFunc(conn))

	// new index, should get a different port
	conn1, err := createUDPConnForConnIndex(1, nil, &logger)
	require.NoError(t, err)
	require.NotEqual(t, initialPort, getPortFunc(conn1))

	// not closing the conn and trying to obtain a new conn for same index should give a different random port
	conn, err = createUDPConnForConnIndex(0, nil, &logger)
	require.NoError(t, err)
	require.NotEqual(t, initialPort, getPortFunc(conn))
}

func serveSession(ctx context.Context, qc *QUICConnection, edgeQUICSession quic.Connection, closeType closeReason, expectedReason string, t *testing.T) {
	var (
		payload = []byte(t.Name())
	)
	sessionID := uuid.New()
	cfdConn, originConn := net.Pipe()
	// Registers and run a new session
	session, err := qc.sessionManager.RegisterSession(ctx, sessionID, cfdConn)
	require.NoError(t, err)

	sessionDone := make(chan struct{})
	go func() {
		qc.serveUDPSession(session, time.Millisecond*50)
		close(sessionDone)
	}()

	// Send a message to the quic session on edge side, it should be deumx to this datagram v2 session
	muxedPayload, err := quicpogs.SuffixSessionID(sessionID, payload)
	require.NoError(t, err)
	muxedPayload, err = quicpogs.SuffixType(muxedPayload, quicpogs.DatagramTypeUDP)
	require.NoError(t, err)

	err = edgeQUICSession.SendDatagram(muxedPayload)
	require.NoError(t, err)

	readBuffer := make([]byte, len(payload)+1)
	n, err := originConn.Read(readBuffer)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	require.True(t, bytes.Equal(payload, readBuffer[:n]))

	// Close connection to terminate session
	switch closeType {
	case closedByOrigin:
		originConn.Close()
	case closedByRemote:
		err = qc.UnregisterUdpSession(ctx, sessionID, expectedReason)
		require.NoError(t, err)
	case closedByTimeout:
	}

	if closeType != closedByRemote {
		// Session was not closed by remote, so closeUDPSession should be invoked to unregister from remote
		unregisterFromEdgeChan := make(chan struct{})
		sessionRPCServer := &mockSessionRPCServer{
			sessionID:            sessionID,
			unregisterReason:     expectedReason,
			calledUnregisterChan: unregisterFromEdgeChan,
		}
		go runRPCServer(ctx, edgeQUICSession, sessionRPCServer, nil, t)

		<-unregisterFromEdgeChan
	}

	<-sessionDone
}

type closeReason uint8

const (
	closedByOrigin closeReason = iota
	closedByRemote
	closedByTimeout
)

func runRPCServer(ctx context.Context, session quic.Connection, sessionRPCServer tunnelpogs.SessionManager, configRPCServer tunnelpogs.ConfigurationManager, t *testing.T) {
	stream, err := session.AcceptStream(ctx)
	require.NoError(t, err)

	if stream.StreamID() == 0 {
		// Skip the first stream, it's the control stream of the QUIC connection
		stream, err = session.AcceptStream(ctx)
		require.NoError(t, err)
	}
	protocol, err := quicpogs.DetermineProtocol(stream)
	assert.NoError(t, err)
	rpcServerStream, err := quicpogs.NewRPCServerStream(stream, protocol)
	assert.NoError(t, err)

	log := zerolog.New(os.Stdout)
	err = rpcServerStream.Serve(sessionRPCServer, configRPCServer, &log)
	assert.NoError(t, err)
}

type mockSessionRPCServer struct {
	sessionID            uuid.UUID
	unregisterReason     string
	calledUnregisterChan chan struct{}
}

func (s mockSessionRPCServer) RegisterUdpSession(ctx context.Context, sessionID uuid.UUID, dstIP net.IP, dstPort uint16, closeIdleAfter time.Duration, traceContext string) (*pogs.RegisterUdpSessionResponse, error) {
	return nil, fmt.Errorf("mockSessionRPCServer doesn't implement RegisterUdpSession")
}

func (s mockSessionRPCServer) UnregisterUdpSession(ctx context.Context, sessionID uuid.UUID, reason string) error {
	if s.sessionID != sessionID {
		return fmt.Errorf("expect session ID %s, got %s", s.sessionID, sessionID)
	}
	if s.unregisterReason != reason {
		return fmt.Errorf("expect unregister reason %s, got %s", s.unregisterReason, reason)
	}
	close(s.calledUnregisterChan)
	return nil
}

func testQUICConnection(udpListenerAddr net.Addr, t *testing.T, index uint8) *QUICConnection {
	tlsClientConfig := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"argotunnel"},
	}
	// Start a mock httpProxy
	log := zerolog.New(os.Stdout)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	qc, err := NewQUICConnection(
		ctx,
		testQUICConfig,
		udpListenerAddr,
		nil,
		index,
		tlsClientConfig,
		&mockOrchestrator{originProxy: &mockOriginProxyWithRequest{}},
		&tunnelpogs.ConnectionOptions{},
		fakeControlStream{},
		&log,
		nil,
		5*time.Second,
		0*time.Second,
	)
	require.NoError(t, err)
	return qc
}

type mockReaderNoopWriter struct {
	io.Reader
}

func (m *mockReaderNoopWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (m *mockReaderNoopWriter) Close() error {
	return nil
}
