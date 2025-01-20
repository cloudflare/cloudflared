package ingress

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	gorillaWS "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/proxy"
	"golang.org/x/sync/errgroup"

	"github.com/cloudflare/cloudflared/socks"
	"github.com/cloudflare/cloudflared/stream"
	"github.com/cloudflare/cloudflared/websocket"
)

const (
	testStreamTimeout = time.Second * 3
	echoHeaderName    = "Test-Cloudflared-Echo"
)

var (
	testMessage  = []byte("TestStreamOriginConnection")
	testResponse = []byte(fmt.Sprintf("echo-%s", testMessage))
)

func TestStreamTCPConnection(t *testing.T) {
	cfdConn, originConn := net.Pipe()
	tcpConn := tcpConnection{
		Conn:         cfdConn,
		writeTimeout: 30 * time.Second,
	}

	eyeballConn, edgeConn := net.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), testStreamTimeout)
	defer cancel()

	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		_, err := eyeballConn.Write(testMessage)
		require.NoError(t, err)

		readBuffer := make([]byte, len(testResponse))
		_, err = eyeballConn.Read(readBuffer)
		require.NoError(t, err)

		require.Equal(t, testResponse, readBuffer)

		return nil
	})
	errGroup.Go(func() error {
		echoTCPOrigin(t, originConn)
		originConn.Close()
		return nil
	})

	tcpConn.Stream(ctx, edgeConn, TestLogger)
	require.NoError(t, errGroup.Wait())
}

func TestDefaultStreamWSOverTCPConnection(t *testing.T) {
	cfdConn, originConn := net.Pipe()
	tcpOverWSConn := tcpOverWSConnection{
		conn:          cfdConn,
		streamHandler: DefaultStreamHandler,
	}

	eyeballConn, edgeConn := net.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), testStreamTimeout)
	defer cancel()

	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		echoWSEyeball(t, eyeballConn)
		return nil
	})
	errGroup.Go(func() error {
		echoTCPOrigin(t, originConn)
		originConn.Close()
		return nil
	})

	tcpOverWSConn.Stream(ctx, edgeConn, TestLogger)
	require.NoError(t, errGroup.Wait())
}

// TestSocksStreamWSOverTCPConnection simulates proxying in socks mode.
// Eyeball side runs cloudflared access tcp with --url flag to start a websocket forwarder which
// wraps SOCKS5 traffic in websocket
// Origin side runs a tcpOverWSConnection with socks.StreamHandler
func TestSocksStreamWSOverTCPConnection(t *testing.T) {
	var (
		sendMessage             = t.Name()
		echoHeaderIncomingValue = fmt.Sprintf("header-%s", sendMessage)
		echoMessage             = fmt.Sprintf("echo-%s", sendMessage)
		echoHeaderReturnValue   = fmt.Sprintf("echo-%s", echoHeaderIncomingValue)
	)

	statusCodes := []int{
		http.StatusOK,
		http.StatusTemporaryRedirect,
		http.StatusBadRequest,
		http.StatusInternalServerError,
	}
	for _, status := range statusCodes {
		handler := func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Equal(t, []byte(sendMessage), body)

			require.Equal(t, echoHeaderIncomingValue, r.Header.Get(echoHeaderName))
			w.Header().Set(echoHeaderName, echoHeaderReturnValue)

			w.WriteHeader(status)
			w.Write([]byte(echoMessage))
		}
		origin := httptest.NewServer(http.HandlerFunc(handler))
		defer origin.Close()

		originURL, err := url.Parse(origin.URL)
		require.NoError(t, err)

		originConn, err := net.Dial("tcp", originURL.Host)
		require.NoError(t, err)

		tcpOverWSConn := tcpOverWSConnection{
			conn:          originConn,
			streamHandler: socks.StreamHandler,
		}

		wsForwarderOutConn, edgeConn := net.Pipe()
		ctx, cancel := context.WithTimeout(context.Background(), testStreamTimeout)
		defer cancel()

		errGroup, ctx := errgroup.WithContext(ctx)
		errGroup.Go(func() error {
			tcpOverWSConn.Stream(ctx, edgeConn, TestLogger)
			return nil
		})

		wsForwarderListener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)

		errGroup.Go(func() error {
			wsForwarderInConn, err := wsForwarderListener.Accept()
			require.NoError(t, err)
			defer wsForwarderInConn.Close()

			stream.Pipe(wsForwarderInConn, &wsEyeball{wsForwarderOutConn}, TestLogger)
			return nil
		})

		eyeballDialer, err := proxy.SOCKS5("tcp", wsForwarderListener.Addr().String(), nil, proxy.Direct)
		require.NoError(t, err)

		transport := &http.Transport{
			Dial: eyeballDialer.Dial,
		}

		// Request URL doesn't matter because the transport is using eyeballDialer to connectq
		req, err := http.NewRequestWithContext(ctx, "GET", "http://test-socks-stream.com", bytes.NewBuffer([]byte(sendMessage)))
		assert.NoError(t, err)
		req.Header.Set(echoHeaderName, echoHeaderIncomingValue)

		resp, err := transport.RoundTrip(req)
		assert.NoError(t, err)
		assert.Equal(t, status, resp.StatusCode)
		require.Equal(t, echoHeaderReturnValue, resp.Header.Get(echoHeaderName))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, []byte(echoMessage), body)

		wsForwarderOutConn.Close()
		edgeConn.Close()
		tcpOverWSConn.Close()

		require.NoError(t, errGroup.Wait())
	}
}

func TestWsConnReturnsBeforeStreamReturns(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eyeballConn := &readWriter{
			w: w,
			r: r.Body,
		}

		cfdConn, originConn := net.Pipe()
		tcpOverWSConn := tcpOverWSConnection{
			conn:          cfdConn,
			streamHandler: DefaultStreamHandler,
		}
		go func() {
			time.Sleep(time.Millisecond * 10)
			// Simulate losing connection to origin
			originConn.Close()
		}()
		ctx := context.WithValue(r.Context(), websocket.PingPeriodContextKey, time.Microsecond)
		tcpOverWSConn.Stream(ctx, eyeballConn, TestLogger)
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	errGroup, ctx := errgroup.WithContext(ctx)
	for i := 0; i < 50; i++ {
		eyeballConn, edgeConn := net.Pipe()
		req, err := http.NewRequestWithContext(ctx, http.MethodConnect, server.URL, edgeConn)
		assert.NoError(t, err)

		resp, err := client.Transport.RoundTrip(req)
		assert.NoError(t, err)
		assert.Equal(t, resp.StatusCode, http.StatusOK)

		errGroup.Go(func() error {
			for {
				if err := wsutil.WriteClientBinary(eyeballConn, testMessage); err != nil {
					return nil
				}
			}
		})
	}

	assert.NoError(t, errGroup.Wait())
}

type wsEyeball struct {
	conn net.Conn
}

func (wse *wsEyeball) Read(p []byte) (int, error) {
	data, err := wsutil.ReadServerBinary(wse.conn)
	if err != nil {
		return 0, err
	}
	return copy(p, data), nil
}

func (wse *wsEyeball) Write(p []byte) (int, error) {
	err := wsutil.WriteClientBinary(wse.conn, p)
	return len(p), err
}

func echoWSEyeball(t *testing.T, conn net.Conn) {
	defer func() {
		assert.NoError(t, conn.Close())
	}()

	if !assert.NoError(t, wsutil.WriteClientBinary(conn, testMessage)) {
		return
	}

	readMsg, err := wsutil.ReadServerBinary(conn)
	if !assert.NoError(t, err) {
		return
	}

	assert.Equal(t, testResponse, readMsg)
}

func echoWSOrigin(t *testing.T, expectMessages bool) *httptest.Server {
	var upgrader = gorillaWS.Upgrader{
		ReadBufferSize:  10,
		WriteBufferSize: 10,
	}

	ws := func(w http.ResponseWriter, r *http.Request) {
		header := make(http.Header)
		for k, vs := range r.Header {
			if k == "Test-Cloudflared-Echo" {
				header[k] = vs
			}
		}
		conn, err := upgrader.Upgrade(w, r, header)
		require.NoError(t, err)
		defer conn.Close()

		sawMessage := false
		for {
			messageType, p, err := conn.ReadMessage()
			if err != nil {
				if expectMessages && !sawMessage {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			assert.Equal(t, testMessage, p)
			sawMessage = true
			if err := conn.WriteMessage(messageType, testResponse); err != nil {
				return
			}
		}
	}

	// NewTLSServer starts the server in another thread
	return httptest.NewTLSServer(http.HandlerFunc(ws))
}

func echoTCPOrigin(t *testing.T, conn net.Conn) {
	readBuffer := make([]byte, len(testMessage))
	_, err := conn.Read(readBuffer)
	assert.NoError(t, err)

	assert.Equal(t, testMessage, readBuffer)

	_, err = conn.Write(testResponse)
	assert.NoError(t, err)
}

type readWriter struct {
	w io.Writer
	r io.Reader
}

func (r *readWriter) Read(p []byte) (n int, err error) {
	return r.r.Read(p)
}

func (r *readWriter) Write(p []byte) (n int, err error) {
	return r.w.Write(p)
}
