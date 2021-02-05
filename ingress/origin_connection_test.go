package ingress

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/logger"
	"github.com/gobwas/ws/wsutil"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

const (
	testStreamTimeout = time.Second * 3
)

var (
	testLogger   = logger.Create(nil)
	testMessage  = []byte("TestStreamOriginConnection")
	testResponse = []byte(fmt.Sprintf("echo-%s", testMessage))
)

func TestStreamTCPConnection(t *testing.T) {
	cfdConn, originConn := net.Pipe()
	tcpConn := tcpConnection{
		conn: cfdConn,
	}

	eyeballConn, edgeConn := net.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), testStreamTimeout)
	defer cancel()

	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		_, err := eyeballConn.Write(testMessage)

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

	tcpConn.Stream(ctx, edgeConn, testLogger)
	require.NoError(t, errGroup.Wait())
}

func TestStreamWSOverTCPConnection(t *testing.T) {
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

	tcpOverWSConn.Stream(ctx, edgeConn, testLogger)
	require.NoError(t, errGroup.Wait())
}

func TestStreamWSConnection(t *testing.T) {
	eyeballConn, edgeConn := net.Pipe()

	origin := echoWSOrigin(t)
	defer origin.Close()

	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	clientTLSConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	wsConn, resp, err := newWSConnection(clientTLSConfig, req)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	require.Equal(t, "Upgrade", resp.Header.Get("Connection"))
	require.Equal(t, "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=", resp.Header.Get("Sec-Websocket-Accept"))
	require.Equal(t, "websocket", resp.Header.Get("Upgrade"))

	ctx, cancel := context.WithTimeout(context.Background(), testStreamTimeout)
	defer cancel()

	errGroup, ctx := errgroup.WithContext(ctx)
	errGroup.Go(func() error {
		echoWSEyeball(t, eyeballConn)
		return nil
	})

	wsConn.Stream(ctx, edgeConn, testLogger)
	require.NoError(t, errGroup.Wait())
}

func echoWSEyeball(t *testing.T, conn net.Conn) {
	require.NoError(t, wsutil.WriteClientBinary(conn, testMessage))

	readMsg, err := wsutil.ReadServerBinary(conn)
	require.NoError(t, err)

	require.Equal(t, testResponse, readMsg)

	require.NoError(t, conn.Close())
}

func echoWSOrigin(t *testing.T) *httptest.Server {
	var upgrader = websocket.Upgrader{
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

		for {
			messageType, p, err := conn.ReadMessage()
			if err != nil {
				return
			}
			require.Equal(t, testMessage, p)
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
