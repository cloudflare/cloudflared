package connection

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	"github.com/google/uuid"
	pkgerrors "github.com/pkg/errors"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/nettest"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/config"
	cfdflow "github.com/cloudflare/cloudflared/flow"

	"github.com/cloudflare/cloudflared/datagramsession"
	"github.com/cloudflare/cloudflared/ingress"
	"github.com/cloudflare/cloudflared/packet"
	cfdquic "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/tracing"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	rpcquic "github.com/cloudflare/cloudflared/tunnelrpc/quic"
)

var (
	testTLSServerConfig = GenerateTLSConfig()
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
	err := wsutil.WriteClientBinary(wsBuf, []byte("Hello"))
	require.NoError(t, err)

	tests := []struct {
		desc             string
		dest             string
		connectionType   pogs.ConnectionType
		metadata         []pogs.Metadata
		message          []byte
		expectedResponse []byte
	}{
		{
			desc:           "test http proxy",
			dest:           "/ok",
			connectionType: pogs.ConnectionTypeHTTP,
			metadata: []pogs.Metadata{
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
			connectionType: pogs.ConnectionTypeHTTP,
			metadata: []pogs.Metadata{
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
			connectionType: pogs.ConnectionTypeWebsocket,
			metadata: []pogs.Metadata{
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
			connectionType:   pogs.ConnectionTypeTCP,
			metadata:         []pogs.Metadata{},
			message:          []byte("Here is some tcp data"),
			expectedResponse: []byte("Here is some tcp data"),
		},
	}

	for i, test := range tests {
		test := test // capture range variable
		t.Run(test.desc, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
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
				// nolint: testifylint
				quicServer(
					ctx, t, quicListener, test.dest, test.connectionType, test.metadata, test.message, test.expectedResponse,
				)
				close(serverDone)
			}()

			// nolint: gosec
			tunnelConn, _ := testTunnelConnection(t, netip.MustParseAddrPort(udpListener.LocalAddr().String()), uint8(i))

			connDone := make(chan struct{})
			go func() {
				_ = tunnelConn.Serve(ctx)
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

func (fakeControlStream) ServeControlStream(ctx context.Context, rw io.ReadWriteCloser, connOptions *pogs.ConnectionOptions, tunnelConfigGetter TunnelConfigJSONGetter) error {
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
	connectionType pogs.ConnectionType,
	metadata []pogs.Metadata,
	message []byte,
	expectedResponse []byte,
) {
	session, err := listener.Accept(ctx)
	require.NoError(t, err)

	quicStream, err := session.OpenStreamSync(t.Context())
	require.NoError(t, err)
	stream := cfdquic.NewSafeStreamCloser(quicStream, defaultQUICTimeout, &log)

	reqClientStream := rpcquic.RequestClientStream{ReadWriteCloser: stream}
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
		time.Sleep(5 * time.Nanosecond)
		fallthrough
	case "/echo_body":
		resp := &http.Response{
			StatusCode: http.StatusOK,
		}
		_ = w.WriteRespHeaders(resp.StatusCode, resp.Header)
		_, _ = io.Copy(w, r.Body)
	case "/error":
		return fmt.Errorf("Failed to proxy to origin")
	default:
		originRespEndpoint(w, http.StatusNotFound, []byte("page not found"))
	}
	return nil
}

func TestBuildHTTPRequest(t *testing.T) {
	tests := []struct {
		name           string
		connectRequest *pogs.ConnectRequest
		body           io.ReadCloser
		req            *http.Request
	}{
		{
			name: "check if http.Request is built correctly with content length",
			connectRequest: &pogs.ConnectRequest{
				Dest: "http://test.com",
				Metadata: []pogs.Metadata{
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
			connectRequest: &pogs.ConnectRequest{
				Dest: "http://test.com",
				Metadata: []pogs.Metadata{
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
			connectRequest: &pogs.ConnectRequest{
				Dest: "http://test.com",
				Metadata: []pogs.Metadata{
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
			connectRequest: &pogs.ConnectRequest{
				Dest: "http://test.com",
				Metadata: []pogs.Metadata{
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
			connectRequest: &pogs.ConnectRequest{
				Type: pogs.ConnectionTypeWebsocket,
				Dest: "http://test.com",
				Metadata: []pogs.Metadata{
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
			req, err := buildHTTPRequest(t.Context(), test.connectRequest, test.body, 0, &log)
			require.NoError(t, err)
			test.req = test.req.WithContext(req.Context())
			require.Equal(t, test.req, req.Request)
		})
	}
}

func (moc *mockOriginProxyWithRequest) ProxyTCP(ctx context.Context, rwa ReadWriteAcker, tcpRequest *TCPRequest) error {
	if tcpRequest.Dest == "rate-limit-me" {
		return pkgerrors.Wrap(cfdflow.ErrTooManyActiveFlows, "failed tcp stream")
	}

	_ = rwa.AckConnection("")
	_, _ = io.Copy(rwa, rwa)
	return nil
}

func TestServeUDPSession(t *testing.T) {
	// Start a UDP Listener for QUIC.
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	require.NoError(t, err)
	udpListener, err := net.ListenUDP(udpAddr.Network(), udpAddr)
	require.NoError(t, err)
	defer udpListener.Close()

	ctx, cancel := context.WithCancel(t.Context())

	// Establish QUIC connection with edge
	edgeQUICSessionChan := make(chan quic.Connection)
	go func() {
		earlyListener, err := quic.Listen(udpListener, testTLSServerConfig, testQUICConfig)
		assert.NoError(t, err)

		edgeQUICSession, err := earlyListener.Accept(ctx)
		assert.NoError(t, err)

		edgeQUICSessionChan <- edgeQUICSession
	}()

	// Random index to avoid reusing port
	tunnelConn, datagramConn := testTunnelConnection(t, netip.MustParseAddrPort(udpListener.LocalAddr().String()), 28)
	go func() {
		_ = tunnelConn.Serve(ctx)
	}()

	edgeQUICSession := <-edgeQUICSessionChan

	serveSession(ctx, datagramConn, edgeQUICSession, closedByOrigin, io.EOF.Error(), t)
	serveSession(ctx, datagramConn, edgeQUICSession, closedByTimeout, datagramsession.SessionIdleErr(time.Millisecond*50).Error(), t)
	serveSession(ctx, datagramConn, edgeQUICSession, closedByRemote, "eyeball closed connection", t)
	cancel()
}

func TestNopCloserReadWriterCloseBeforeEOF(t *testing.T) {
	readerWriter := nopCloserReadWriter{ReadWriteCloser: &mockReaderNoopWriter{Reader: strings.NewReader("123456789")}}
	buffer := make([]byte, 5)

	n, err := readerWriter.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, 5, n)

	// close
	require.NoError(t, readerWriter.Close())

	// read should get error
	n, err = readerWriter.Read(buffer)
	require.Equal(t, 0, n)
	require.Equal(t, err, fmt.Errorf("closed by handler"))
}

func TestNopCloserReadWriterCloseAfterEOF(t *testing.T) {
	readerWriter := nopCloserReadWriter{ReadWriteCloser: &mockReaderNoopWriter{Reader: strings.NewReader("123456789")}}
	buffer := make([]byte, 20)

	n, err := readerWriter.Read(buffer)
	require.NoError(t, err)
	require.Equal(t, 9, n)

	// force another read to read eof
	_, err = readerWriter.Read(buffer)
	require.Equal(t, err, io.EOF)

	// close
	require.NoError(t, readerWriter.Close())

	// read should get EOF still
	n, err = readerWriter.Read(buffer)
	require.Equal(t, 0, n)
	require.Equal(t, err, io.EOF)
}

func TestCreateUDPConnReuseSourcePort(t *testing.T) {
	edgeIPv4 := netip.MustParseAddrPort("0.0.0.0:0")
	edgeIPv6 := netip.MustParseAddrPort("[::]:0")

	// We assume the test environment has access to an IPv4 interface
	testCreateUDPConnReuseSourcePortForEdgeIP(t, edgeIPv4)

	if nettest.SupportsIPv6() {
		testCreateUDPConnReuseSourcePortForEdgeIP(t, edgeIPv6)
	}
}

// TestTCPProxy_FlowRateLimited tests if the pogs.ConnectResponse returns the expected error and metadata, when a
// new flow is rate limited.
func TestTCPProxy_FlowRateLimited(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())

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
		defer close(serverDone)

		session, err := quicListener.Accept(ctx)
		assert.NoError(t, err)

		quicStream, err := session.OpenStreamSync(t.Context())
		assert.NoError(t, err)
		stream := cfdquic.NewSafeStreamCloser(quicStream, defaultQUICTimeout, &log)

		reqClientStream := rpcquic.RequestClientStream{ReadWriteCloser: stream}
		err = reqClientStream.WriteConnectRequestData("rate-limit-me", pogs.ConnectionTypeTCP)
		assert.NoError(t, err)

		response, err := reqClientStream.ReadConnectResponseData()
		assert.NoError(t, err)

		// Got Rate Limited
		assert.NotEmpty(t, response.Error)
		assert.Contains(t, response.Metadata, pogs.ErrorFlowConnectRateLimitedMetadata)
	}()

	tunnelConn, _ := testTunnelConnection(t, netip.MustParseAddrPort(udpListener.LocalAddr().String()), uint8(0))

	connDone := make(chan struct{})
	go func() {
		defer close(connDone)
		_ = tunnelConn.Serve(ctx)
	}()

	<-serverDone
	cancel()
	<-connDone
}

func testCreateUDPConnReuseSourcePortForEdgeIP(t *testing.T, edgeIP netip.AddrPort) {
	logger := zerolog.Nop()
	conn, err := createUDPConnForConnIndex(0, nil, edgeIP, &logger)
	require.NoError(t, err)

	getPortFunc := func(conn *net.UDPConn) int {
		addr := conn.LocalAddr().(*net.UDPAddr)
		return addr.Port
	}

	initialPort := getPortFunc(conn)

	// close conn
	conn.Close()

	// should get the same port as before.
	conn, err = createUDPConnForConnIndex(0, nil, edgeIP, &logger)
	require.NoError(t, err)
	require.Equal(t, initialPort, getPortFunc(conn))

	// new index, should get a different port
	conn1, err := createUDPConnForConnIndex(1, nil, edgeIP, &logger)
	require.NoError(t, err)
	require.NotEqual(t, initialPort, getPortFunc(conn1))

	// not closing the conn and trying to obtain a new conn for same index should give a different random port
	conn, err = createUDPConnForConnIndex(0, nil, edgeIP, &logger)
	require.NoError(t, err)
	require.NotEqual(t, initialPort, getPortFunc(conn))
}

func serveSession(ctx context.Context, datagramConn *datagramV2Connection, edgeQUICSession quic.Connection, closeType closeReason, expectedReason string, t *testing.T) {
	payload := []byte(t.Name())
	sessionID := uuid.New()
	cfdConn, originConn := net.Pipe()
	// Registers and run a new session
	session, err := datagramConn.sessionManager.RegisterSession(ctx, sessionID, cfdConn)
	require.NoError(t, err)

	sessionDone := make(chan struct{})
	go func() {
		datagramConn.serveUDPSession(session, time.Millisecond*50)
		close(sessionDone)
	}()

	// Send a message to the quic session on edge side, it should be deumx to this datagram v2 session
	muxedPayload, err := cfdquic.SuffixSessionID(sessionID, payload)
	require.NoError(t, err)
	muxedPayload, err = cfdquic.SuffixType(muxedPayload, cfdquic.DatagramTypeUDP)
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
		err = datagramConn.UnregisterUdpSession(ctx, sessionID, expectedReason)
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
		// nolint: testifylint
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

func runRPCServer(ctx context.Context, session quic.Connection, sessionRPCServer pogs.SessionManager, configRPCServer pogs.ConfigurationManager, t *testing.T) {
	stream, err := session.AcceptStream(ctx)
	require.NoError(t, err)

	if stream.StreamID() == 0 {
		// Skip the first stream, it's the control stream of the QUIC connection
		stream, err = session.AcceptStream(ctx)
		require.NoError(t, err)
	}
	ss := rpcquic.NewCloudflaredServer(
		func(_ context.Context, _ *rpcquic.RequestServerStream) error {
			return nil
		},
		sessionRPCServer,
		configRPCServer,
		10*time.Second,
	)
	err = ss.Serve(ctx, stream)
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

func testTunnelConnection(t *testing.T, serverAddr netip.AddrPort, index uint8) (TunnelConnection, *datagramV2Connection) {
	tlsClientConfig := &tls.Config{
		// nolint: gosec
		InsecureSkipVerify: true,
		NextProtos:         []string{"argotunnel"},
	}
	// Start a mock httpProxy
	log := zerolog.New(io.Discard)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// Dial the QUIC connection to the edge
	conn, err := DialQuic(
		ctx,
		testQUICConfig,
		tlsClientConfig,
		serverAddr,
		nil, // connect on a random port
		index,
		&log,
	)
	require.NoError(t, err)

	// Start a session manager for the connection
	sessionDemuxChan := make(chan *packet.Session, 4)
	datagramMuxer := cfdquic.NewDatagramMuxerV2(conn, &log, sessionDemuxChan)
	sessionManager := datagramsession.NewManager(&log, datagramMuxer.SendToSession, sessionDemuxChan)
	var connIndex uint8 = 0
	packetRouter := ingress.NewPacketRouter(nil, datagramMuxer, connIndex, &log)
	testDefaultDialer := ingress.NewDialer(ingress.WarpRoutingConfig{
		ConnectTimeout: config.CustomDuration{Duration: 1 * time.Second},
		TCPKeepAlive:   config.CustomDuration{Duration: 15 * time.Second},
		MaxActiveFlows: 0,
	})
	originDialer := ingress.NewOriginDialer(ingress.OriginConfig{
		DefaultDialer:   testDefaultDialer,
		TCPWriteTimeout: 1 * time.Second,
	}, &log)

	datagramConn := &datagramV2Connection{
		conn,
		index,
		sessionManager,
		cfdflow.NewLimiter(0),
		datagramMuxer,
		originDialer,
		packetRouter,
		15 * time.Second,
		0 * time.Second,
		&log,
	}

	tunnelConn := NewTunnelConnection(
		ctx,
		conn,
		index,
		&mockOrchestrator{originProxy: &mockOriginProxyWithRequest{}},
		datagramConn,
		fakeControlStream{},
		&client.ConnectionOptionsSnapshot{},
		15*time.Second,
		0*time.Second,
		0*time.Second,
		&log,
	)
	return tunnelConn, datagramConn
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

// GenerateTLSConfig sets up a bare-bones TLS config for a QUIC server
func GenerateTLSConfig() *tls.Config {
	// nolint: gosec
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	// nolint: gosec
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"argotunnel"},
	}
}
