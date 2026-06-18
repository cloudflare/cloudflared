package carrier

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/rand"
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
	assert.NoError(t, err)
	certPool.AddCert(helloCert)
	assert.NotNil(t, certPool)
	return &tls.Config{RootCAs: certPool}
}

func TestWebsocketHeaders(t *testing.T) {
	req := testRequest(t, "http://example.com", nil)
	wsHeaders := websocketHeaders(req)
	for _, header := range stripWebsocketHeaders {
		assert.Empty(t, wsHeaders[header])
	}
	assert.Equal(t, "curl/7.59.0", wsHeaders.Get("User-Agent"))
}

func TestServe(t *testing.T) {
	log := zerolog.Nop()
	shutdownC := make(chan struct{})
	errC := make(chan error)
	listener, err := hello.CreateTLSListener("localhost:1111")
	assert.NoError(t, err)
	defer listener.Close()

	go func() {
		errC <- hello.StartHelloWorldServer(&log, listener, shutdownC)
	}()

	req := testRequest(t, "https://localhost:1111/ws", nil)

	tlsConfig := websocketClientTLSConfig(t)
	assert.NotNil(t, tlsConfig)
	d := gws.Dialer{TLSClientConfig: tlsConfig}
	conn, resp, err := clientConnect(req, &d)
	assert.NoError(t, err)
	assert.Equal(t, "websocket", resp.Header.Get("Upgrade"))

	for i := 0; i < 1000; i++ {
		messageSize := rand.Int()%2048 + 1
		clientMessage := make([]byte, messageSize)
		// rand.Read always returns len(clientMessage) and a nil error
		rand.Read(clientMessage)
		err = conn.WriteMessage(websocket.BinaryFrame, clientMessage)
		assert.NoError(t, err)

		messageType, message, err := conn.ReadMessage()
		assert.NoError(t, err)
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
	assert.Equal(t, "websocket", resp.Header.Get("Upgrade"))

	// Websocket now connected to test server so lets check our wrapper
	wrapper := cfwebsocket.GorillaConn{Conn: conn}
	buf := make([]byte, 100)
	wrapper.Write([]byte("abc"))
	n, err := wrapper.Read(buf)
	require.NoError(t, err)
	require.Equal(t, n, 3)
	require.Equal(t, "abc", string(buf[:n]))

	// Test partial read, read 1 of 3 bytes in one read and the other 2 in another read
	wrapper.Write([]byte("abc"))
	buf = buf[:1]
	n, err = wrapper.Read(buf)
	require.NoError(t, err)
	require.Equal(t, n, 1)
	require.Equal(t, "a", string(buf[:n]))
	buf = buf[:cap(buf)]
	n, err = wrapper.Read(buf)
	require.NoError(t, err)
	require.Equal(t, n, 2)
	require.Equal(t, "bc", string(buf[:n]))
}
