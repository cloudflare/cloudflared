package connection

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/tracing"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/websocket"
)

const (
	largeFileSize   = 2 * 1024 * 1024
	testGracePeriod = time.Millisecond * 100
)

var (
	testOrchestrator = &mockOrchestrator{
		originProxy: &mockOriginProxy{},
	}
	log           = zerolog.Nop()
	testLargeResp = make([]byte, largeFileSize)
)

type testRequest struct {
	name           string
	endpoint       string
	expectedStatus int
	expectedBody   []byte
	isProxyError   bool
}

type mockOrchestrator struct {
	originProxy OriginProxy
}

func (mcr *mockOrchestrator) GetConfigJSON() ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (*mockOrchestrator) UpdateConfig(version int32, config []byte) *tunnelpogs.UpdateConfigurationResponse {
	return &tunnelpogs.UpdateConfigurationResponse{
		LastAppliedVersion: version,
	}
}

func (mcr *mockOrchestrator) GetOriginProxy() (OriginProxy, error) {
	return mcr.originProxy, nil
}

type mockOriginProxy struct{}

func (moc *mockOriginProxy) ProxyHTTP(
	w ResponseWriter,
	tr *tracing.TracedRequest,
	isWebsocket bool,
) error {
	req := tr.Request
	if isWebsocket {
		switch req.URL.Path {
		case "/ws/echo":
			return wsEchoEndpoint(w, req)
		case "/ws/flaky":
			return wsFlakyEndpoint(w, req)
		default:
			originRespEndpoint(w, http.StatusNotFound, []byte("ws endpoint not found"))
			return fmt.Errorf("Unknwon websocket endpoint %s", req.URL.Path)
		}
	}
	switch req.URL.Path {
	case "/ok":
		originRespEndpoint(w, http.StatusOK, []byte(http.StatusText(http.StatusOK)))
	case "/large_file":
		originRespEndpoint(w, http.StatusOK, testLargeResp)
	case "/400":
		originRespEndpoint(w, http.StatusBadRequest, []byte(http.StatusText(http.StatusBadRequest)))
	case "/500":
		originRespEndpoint(w, http.StatusInternalServerError, []byte(http.StatusText(http.StatusInternalServerError)))
	case "/error":
		return fmt.Errorf("Failed to proxy to origin")
	default:
		originRespEndpoint(w, http.StatusNotFound, []byte("page not found"))
	}
	return nil

}

func (moc *mockOriginProxy) ProxyTCP(
	ctx context.Context,
	rwa ReadWriteAcker,
	r *TCPRequest,
) error {
	return nil
}

type echoPipe struct {
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (ep *echoPipe) Read(p []byte) (int, error) {
	return ep.reader.Read(p)
}

func (ep *echoPipe) Write(p []byte) (int, error) {
	return ep.writer.Write(p)
}

// A mock origin that echos data by streaming like a tcpOverWSConnection
// https://github.com/cloudflare/cloudflared/blob/master/ingress/origin_connection.go
func wsEchoEndpoint(w ResponseWriter, r *http.Request) error {
	resp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
	}
	if err := w.WriteRespHeaders(resp.StatusCode, resp.Header); err != nil {
		return err
	}
	wsCtx, cancel := context.WithCancel(r.Context())
	readPipe, writePipe := io.Pipe()
	wsConn := websocket.NewConn(wsCtx, NewHTTPResponseReadWriterAcker(w, r), &log)
	go func() {
		select {
		case <-wsCtx.Done():
		case <-r.Context().Done():
		}
		readPipe.Close()
		writePipe.Close()
	}()

	originConn := &echoPipe{reader: readPipe, writer: writePipe}
	websocket.Stream(wsConn, originConn, &log)
	cancel()
	wsConn.Close()
	return nil
}

type flakyConn struct {
	closeAt time.Time
}

func (fc *flakyConn) Read(p []byte) (int, error) {
	if time.Now().After(fc.closeAt) {
		return 0, io.EOF
	}
	n := copy(p, "Read from flaky connection")
	return n, nil
}

func (fc *flakyConn) Write(p []byte) (int, error) {
	if time.Now().After(fc.closeAt) {
		return 0, fmt.Errorf("flaky connection closed")
	}
	return len(p), nil
}

func wsFlakyEndpoint(w ResponseWriter, r *http.Request) error {
	resp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
	}
	if err := w.WriteRespHeaders(resp.StatusCode, resp.Header); err != nil {
		return err
	}
	wsCtx, cancel := context.WithCancel(r.Context())

	wsConn := websocket.NewConn(wsCtx, NewHTTPResponseReadWriterAcker(w, r), &log)

	closedAfter := time.Millisecond * time.Duration(rand.Intn(50))
	originConn := &flakyConn{closeAt: time.Now().Add(closedAfter)}
	websocket.Stream(wsConn, originConn, &log)
	cancel()
	wsConn.Close()
	return nil
}

func originRespEndpoint(w ResponseWriter, status int, data []byte) {
	resp := &http.Response{
		StatusCode: status,
	}
	_ = w.WriteRespHeaders(resp.StatusCode, resp.Header)
	_, _ = w.Write(data)
}

type mockConnectedFuse struct{}

func (mcf mockConnectedFuse) Connected() {}

func (mcf mockConnectedFuse) IsConnected() bool {
	return true
}

func TestIsEventStream(t *testing.T) {
	tests := []struct {
		headers       http.Header
		isEventStream bool
	}{
		{
			headers:       newHeader("Content-Type", "text/event-stream"),
			isEventStream: true,
		},
		{
			headers:       newHeader("content-type", "text/event-stream"),
			isEventStream: true,
		},
		{
			headers:       newHeader("Content-Type", "text/event-stream; charset=utf-8"),
			isEventStream: true,
		},
		{
			headers:       newHeader("Content-Type", "application/json"),
			isEventStream: false,
		},
		{
			headers:       http.Header{},
			isEventStream: false,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.isEventStream, IsServerSentEvent(test.headers))
	}
}

func newHeader(key, value string) http.Header {
	header := http.Header{}
	header.Add(key, value)
	return header
}
