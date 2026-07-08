package carrier

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/big"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"

	"github.com/cloudflare/cloudflared/hello"
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
