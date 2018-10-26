package carrier

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	ws "github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

const (
	// example in Sec-Websocket-Key in rfc6455
	testSecWebsocketKey = "dGhlIHNhbXBsZSBub25jZQ=="
)

type testStreamer struct {
	buf *bytes.Buffer
	l   sync.RWMutex
}

func newTestStream() *testStreamer {
	return &testStreamer{buf: new(bytes.Buffer)}
}

func (s *testStreamer) Read(p []byte) (int, error) {
	s.l.RLock()
	defer s.l.RUnlock()
	return s.buf.Read(p)

}

func (s *testStreamer) Write(p []byte) (int, error) {
	s.l.Lock()
	defer s.l.Unlock()
	return s.buf.Write(p)
}

func TestStartClient(t *testing.T) {
	message := "Good morning Austin! Time for another sunny day in the great state of Texas."
	logger := logrus.New()
	ts := newTestWebSocketServer()
	defer ts.Close()

	buf := newTestStream()
	err := StartClient(logger, "http://"+ts.Listener.Addr().String(), buf)
	assert.NoError(t, err)
	buf.Write([]byte(message))

	readBuffer := make([]byte, len(message))
	buf.Read(readBuffer)
	assert.Equal(t, message, string(readBuffer))
}

func TestStartServer(t *testing.T) {
	listenerAddress := "localhost:1117"
	message := "Good morning Austin! Time for another sunny day in the great state of Texas."
	logger := logrus.New()
	shutdownC := make(chan struct{})
	ts := newTestWebSocketServer()
	defer ts.Close()

	go func() {
		err := StartServer(logger, listenerAddress, "http://"+ts.Listener.Addr().String(), shutdownC)
		if err != nil {
			t.Fatalf("Error starting server: %v", err)
		}
	}()

	conn, err := net.Dial("tcp", listenerAddress)
	if err != nil {
		t.Fatalf("Error connecting to server: %v", err)
	}
	conn.Write([]byte(message))

	readBuffer := make([]byte, len(message))
	conn.Read(readBuffer)
	assert.Equal(t, string(readBuffer), message)
}

func newTestWebSocketServer() *httptest.Server {
	upgrader := ws.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()
		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			if err := conn.WriteMessage(mt, []byte(message)); err != nil {
				break
			}
		}
	}))
}

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
