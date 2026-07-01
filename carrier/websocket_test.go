package carrier

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/stream"
	"github.com/cloudflare/cloudflared/tlsconfig"
	cfwebsocket "github.com/cloudflare/cloudflared/websocket"
)

func websocketClientTLSConfig(t *testing.T) *tls.Config {
	certPool := x509.NewCertPool()
	helloCert, err := tlsconfig.GetHelloCertificateX509()
	require.NoError(t, err)
	certPool.AddCert(helloCert)
	assert.NotNil(t, certPool)
	return &tls.Config{RootCAs: certPool}
}

func TestServe(t *testing.T) {
	log := zerolog.Nop()
	shutdownC := make(chan struct{})
	errC := make(chan error)
	listener, err := hello.CreateTLSListener("localhost:1111")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	go func() {
		errC <- hello.StartHelloWorldServer(&log, listener, shutdownC)
	}()

	req := testRequest(t, "https://localhost:1111/ws", nil)

	tlsConfig := websocketClientTLSConfig(t)
	assert.NotNil(t, tlsConfig)
	d := gws.Dialer{TLSClientConfig: tlsConfig}
	conn, resp, err := clientConnect(req, &d)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, "websocket", resp.Header.Get("Upgrade"))

	for range 1000 {
		messageSize, err := rand.Int(rand.Reader, big.NewInt(2048))
		require.NoError(t, err)
		clientMessage := make([]byte, messageSize.Int64()+1)
		for i := range clientMessage {
			n, err := rand.Int(rand.Reader, big.NewInt(256))
			n8 := uint8(n.Uint64()) //nolint:gosec // test-only
			require.NoError(t, err)
			clientMessage[i] = n8
		}
		err = conn.WriteMessage(websocket.BinaryFrame, clientMessage)
		require.NoError(t, err)

		messageType, message, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, websocket.BinaryFrame, messageType)
		assert.Equal(t, clientMessage, message)
	}

	_ = conn.Close()
	close(shutdownC)
	<-errC
}

func TestWebsocketWrapper(t *testing.T) {
	listener, err := hello.CreateTLSListener("localhost:0")
	require.NoError(t, err)

	serverErrorChan := make(chan error)
	helloSvrCtx, cancelHelloSvr := context.WithCancel(context.Background())
	defer func() { <-serverErrorChan }()
	defer cancelHelloSvr()
	go func() {
		log := zerolog.Nop()
		serverErrorChan <- hello.StartHelloWorldServer(&log, listener, helloSvrCtx.Done())
	}()

	tlsConfig := websocketClientTLSConfig(t)
	d := gws.Dialer{TLSClientConfig: tlsConfig, HandshakeTimeout: time.Minute}
	testAddr := fmt.Sprintf("https://%s/ws", listener.Addr().String())
	req := testRequest(t, testAddr, nil)
	conn, resp, err := clientConnect(req, &d)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, "websocket", resp.Header.Get("Upgrade"))

	// Websocket now connected to test server so lets check our wrapper
	wrapper := cfwebsocket.GorillaConn{Conn: conn}
	buf := make([]byte, 100)
	_, err = wrapper.Write([]byte("abc"))
	require.NoError(t, err)
	n, err := wrapper.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, "abc", string(buf[:n]))

	// Test partial read, read 1 of 3 bytes in one read and the other 2 in another read
	_, err = wrapper.Write([]byte("abc"))
	require.NoError(t, err)
	buf = buf[:1]
	n, err = wrapper.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "a", string(buf[:n]))
	buf = buf[:cap(buf)]
	n, err = wrapper.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, "bc", string(buf[:n]))
}

// halfClosePipeEnd is a bidirectional pipe built from two independent io.Pipe
// pairs. Unlike net.Pipe, closing one direction does not affect the other, which
// lets us simulate a half-close: the local client can signal EOF on its write
// side while still reading a delayed response from the remote side.
type halfClosePipeEnd struct {
	r *io.PipeReader // data flowing into this end
	w *io.PipeWriter // data flowing out of this end
}

func newHalfClosePipe() (local, remote *halfClosePipeEnd) {
	// Pipe A carries data from local → remote.
	ar, aw := io.Pipe()
	// Pipe B carries data from remote → local.
	br, bw := io.Pipe()

	local = &halfClosePipeEnd{r: br, w: aw}
	remote = &halfClosePipeEnd{r: ar, w: bw}
	return local, remote
}

func (p *halfClosePipeEnd) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *halfClosePipeEnd) Write(b []byte) (int, error) { return p.w.Write(b) }

// CloseWrite signals EOF to the remote reader without closing the read side.
func (p *halfClosePipeEnd) CloseWrite() error { return p.w.Close() }

// Close shuts down both directions.
func (p *halfClosePipeEnd) Close() error {
	_ = p.r.Close()
	return p.w.Close()
}

// TestServeStreamWaitsForResponseAfterLocalClose exercises the websocket path
// It verifies that the pipe does not tear down immediately after the client closes
// its write side.
//
// The setup mirrors the cloudflared access tcp path:
//
//	local app -> halfClosePipe -> ServeStream -> mock WS echo server
//
// Sequence:
//  1. Write payload to the mock server through ServeStream.
//  2. Half-close the write side (CloseWrite) — signals EOF upstream.
//  3. Sleep for halfCloseWait to make the race window explicit: a buggy
//     (timeout=0) pipe would already have torn down the connection here.
//  4. Read the echo — must succeed because the patch keeps the pipe alive.
func TestServeStreamWaitsForResponseAfterLocalClose(t *testing.T) {
	t.Parallel()

	const (
		payload = "half-close-test"
		// halfCloseWait makes the race window visible: if ServeStream tears
		// down the connection on CloseWrite the read that follows will fail
		// immediately, mirroring the 3-second sleep in the cftunnel reference
		// test. It must be shorter than DefaultTimeoutAfterFirstClose (10s).
		halfCloseWait = 3 * time.Second
		// testTimeout is an upper bound for the whole test.
		testTimeout = stream.DefaultTimeoutAfterFirstClose + 5*time.Second
	)

	server := websocketServer()
	defer server.Close()

	localEnd, remoteEnd := newHalfClosePipe()

	log := zerolog.Nop()
	wsConn := NewWSConnection(&log)
	options := &StartOptions{
		OriginURL: "ws://" + server.Listener.Addr().String(),
	}

	serveErrCh := make(chan error, 1)
	go func() {
		serveErrCh <- wsConn.ServeStream(options, remoteEnd)
	}()

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()

	// 1. Write the payload. ServeStream forwards it as a WS binary frame.
	_, err := localEnd.Write([]byte(payload))
	require.NoError(t, err)

	// 2. Half-close the write side
	require.NoError(t, localEnd.CloseWrite())

	// 3. Wait to make the race window explicit.
	time.Sleep(halfCloseWait)

	// 4. Read the echo.
	got := make([]byte, len(payload))
	_, err = io.ReadFull(localEnd, got)
	require.NoError(t, err, "read after half-close failed: pipe was torn down too early")
	require.Equal(t, payload, string(got))

	// Drain ServeStream.
	_ = localEnd.Close()
	_ = remoteEnd.Close()

	select {
	case err := <-serveErrCh:
		if err != nil && err != io.EOF && !errors.Is(err, io.ErrClosedPipe) {
			require.NoError(t, err)
		}
	case <-ctx.Done():
		t.Fatal("ServeStream did not return in time")
	}
}

func websocketServer() *httptest.Server {
	upgrader := gws.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(*http.Request) bool { return true },
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		if err := conn.WriteMessage(gws.BinaryMessage, msg); err != nil {
			return
		}
	}))
}
