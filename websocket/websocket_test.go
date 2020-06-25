package websocket

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/rand"
	"net/http"
	"testing"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/websocket"
)

const (
	// example in Sec-Websocket-Key in rfc6455
	testSecWebsocketKey = "dGhlIHNhbXBsZSBub25jZQ=="
	// example Sec-Websocket-Accept in rfc6455
	testSecWebsocketAccept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
)

func testRequest(t *testing.T, url string, stream io.ReadWriter) *http.Request {
	req, err := http.NewRequest("GET", url, stream)
	if err != nil {
		t.Fatalf("testRequestHeader error")
	}

	req.Header.Add("Connection", "Upgrade")
	req.Header.Add("Upgrade", "WebSocket")
	req.Header.Add("Sec-Websocket-Key", testSecWebsocketKey)
	req.Header.Add("Sec-Websocket-Protocol", "tunnel-protocol")
	req.Header.Add("Sec-Websocket-Version", "13")
	req.Header.Add("User-Agent", "curl/7.59.0")

	return req
}

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

func TestGenerateAcceptKey(t *testing.T) {
	req := testRequest(t, "http://example.com", nil)
	assert.Equal(t, testSecWebsocketAccept, generateAcceptKey(req))
}

func TestServe(t *testing.T) {
	logger := logger.NewOutputWriter(logger.NewMockWriteManager())
	shutdownC := make(chan struct{})
	errC := make(chan error)
	listener, err := hello.CreateTLSListener("localhost:1111")
	assert.NoError(t, err)
	defer listener.Close()

	go func() {
		errC <- hello.StartHelloWorldServer(logger, listener, shutdownC)
	}()

	req := testRequest(t, "https://localhost:1111/ws", nil)

	tlsConfig := websocketClientTLSConfig(t)
	assert.NotNil(t, tlsConfig)
	conn, resp, err := ClientConnect(req, tlsConfig)
	assert.NoError(t, err)
	assert.Equal(t, testSecWebsocketAccept, resp.Header.Get("Sec-WebSocket-Accept"))

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

	conn.Close()
	close(shutdownC)
	<-errC
}

// func TestStartProxyServer(t *testing.T) {
// 	var wg sync.WaitGroup
// 	remoteAddress := "localhost:1113"
// 	listenerAddress := "localhost:1112"
// 	message := "Good morning Austin! Time for another sunny day in the great state of Texas."
// 	logger := logger.NewOutputWriter(logger.NewMockWriteManager())
// 	shutdownC := make(chan struct{})

// 	listener, err := net.Listen("tcp", listenerAddress)
// 	assert.NoError(t, err)
// 	defer listener.Close()

// 	remoteListener, err := net.Listen("tcp", remoteAddress)
// 	assert.NoError(t, err)
// 	defer remoteListener.Close()

// 	wg.Add(1)
// 	go func() {
// 		defer wg.Done()
// 		conn, err := remoteListener.Accept()
// 		assert.NoError(t, err)
// 		buf := make([]byte, len(message))
// 		conn.Read(buf)
// 		assert.Equal(t, string(buf), message)
// 	}()

// 	go func() {
// 		StartProxyServer(logger, listener, remoteAddress, shutdownC)
// 	}()

// 	req := testRequest(t, fmt.Sprintf("http://%s/", listenerAddress), nil)
// 	conn, _, err := ClientConnect(req, nil)
// 	assert.NoError(t, err)
// 	err = conn.WriteMessage(1, []byte(message))
// 	assert.NoError(t, err)
// 	wg.Wait()
// }
