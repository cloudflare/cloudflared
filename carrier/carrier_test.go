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
	"github.com/rs/zerolog"
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
	log := zerolog.Nop()
	wsConn := NewWSConnection(&log)
	ts := newTestWebSocketServer()
	defer ts.Close()

	buf := newTestStream()
	options := &StartOptions{
		OriginURL: "http://" + ts.Listener.Addr().String(),
		Headers:   nil,
	}
	err := StartClient(wsConn, buf, options)
	assert.NoError(t, err)
	_, _ = buf.Write([]byte(message))

	readBuffer := make([]byte, len(message))
	_, _ = buf.Read(readBuffer)
	assert.Equal(t, message, string(readBuffer))
}

func TestStartServer(t *testing.T) {
	listener, err := net.Listen("tcp", "localhost:")
	if err != nil {
		t.Fatalf("Error starting listener: %v", err)
	}
	message := "Good morning Austin! Time for another sunny day in the great state of Texas."
	log := zerolog.Nop()
	shutdownC := make(chan struct{})
	wsConn := NewWSConnection(&log)
	ts := newTestWebSocketServer()
	defer ts.Close()
	options := &StartOptions{
		OriginURL: "http://" + ts.Listener.Addr().String(),
		Headers:   nil,
	}

	go func() {
		err := Serve(wsConn, listener, shutdownC, options)
		if err != nil {
			t.Errorf("Error running server: %v", err)
			return
		}
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	_, _ = conn.Write([]byte(message))

	readBuffer := make([]byte, len(message))
	_, _ = conn.Read(readBuffer)
	assert.Equal(t, string(readBuffer), message)
}

func TestIsAccessResponse(t *testing.T) {
	validLocationHeader := http.Header{}
	validLocationHeader.Add("location", "https://test.cloudflareaccess.com/cdn-cgi/access/login/blahblah")
	invalidLocationHeader := http.Header{}
	invalidLocationHeader.Add("location", "https://google.com")
	testCases := []struct {
		Description string
		In          *http.Response
		ExpectedOut bool
	}{
		{"nil response", nil, false},
		{"redirect with no location", &http.Response{StatusCode: http.StatusFound}, false},
		{"200 ok", &http.Response{StatusCode: http.StatusOK}, false},
		{"redirect with location", &http.Response{StatusCode: http.StatusFound, Header: validLocationHeader}, true},
		{"redirect with invalid location", &http.Response{StatusCode: http.StatusFound, Header: invalidLocationHeader}, false},
	}

	for i, tc := range testCases {
		if IsAccessResponse(tc.In) != tc.ExpectedOut {
			t.Fatalf("Failed case %d -- %s", i, tc.Description)
		}
	}

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

func TestBastionDestination(t *testing.T) {
	tests := []struct {
		name         string
		header       http.Header
		expectedDest string
		wantErr      bool
	}{
		{
			name: "hostname destination",
			header: http.Header{
				cfJumpDestinationHeader: []string{"localhost"},
			},
			expectedDest: "localhost",
		},
		{
			name: "hostname destination with port",
			header: http.Header{
				cfJumpDestinationHeader: []string{"localhost:9000"},
			},
			expectedDest: "localhost:9000",
		},
		{
			name: "hostname destination with scheme and port",
			header: http.Header{
				cfJumpDestinationHeader: []string{"ssh://localhost:9000"},
			},
			expectedDest: "localhost:9000",
		},
		{
			name: "full hostname url",
			header: http.Header{
				cfJumpDestinationHeader: []string{"ssh://localhost:9000/metrics"},
			},
			expectedDest: "localhost:9000",
		},
		{
			name: "hostname destination with port and path",
			header: http.Header{
				cfJumpDestinationHeader: []string{"localhost:9000/metrics"},
			},
			expectedDest: "localhost:9000",
		},
		{
			name: "ip destination",
			header: http.Header{
				cfJumpDestinationHeader: []string{"127.0.0.1"},
			},
			expectedDest: "127.0.0.1",
		},
		{
			name: "ip destination with port",
			header: http.Header{
				cfJumpDestinationHeader: []string{"127.0.0.1:9000"},
			},
			expectedDest: "127.0.0.1:9000",
		},
		{
			name: "ip destination with port and path",
			header: http.Header{
				cfJumpDestinationHeader: []string{"127.0.0.1:9000/metrics"},
			},
			expectedDest: "127.0.0.1:9000",
		},
		{
			name: "ip destination with schem and port",
			header: http.Header{
				cfJumpDestinationHeader: []string{"tcp://127.0.0.1:9000"},
			},
			expectedDest: "127.0.0.1:9000",
		},
		{
			name: "full ip url",
			header: http.Header{
				cfJumpDestinationHeader: []string{"ssh://127.0.0.1:9000/metrics"},
			},
			expectedDest: "127.0.0.1:9000",
		},
		{
			name:    "no destination",
			wantErr: true,
		},
	}
	for _, test := range tests {
		r := &http.Request{
			Header: test.header,
		}
		dest, err := ResolveBastionDest(r)
		if test.wantErr {
			assert.Error(t, err, "Test %s expects error", test.name)
		} else {
			assert.NoError(t, err, "Test %s expects no error, got error %v", test.name, err)
			assert.Equal(t, test.expectedDest, dest, "Test %s expect dest %s, got %s", test.name, test.expectedDest, dest)
		}
	}
}
