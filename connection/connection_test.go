package connection

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

const (
	largeFileSize = 2 * 1024 * 1024
)

var (
	testConfig = &Config{
		OriginProxy: &mockOriginProxy{},
		GracePeriod: time.Millisecond * 100,
	}
	log           = zerolog.Nop()
	testOriginURL = &url.URL{
		Scheme: "https",
		Host:   "connectiontest.argotunnel.com",
	}
	testLargeResp = make([]byte, largeFileSize)
)

type testRequest struct {
	name           string
	endpoint       string
	expectedStatus int
	expectedBody   []byte
	isProxyError   bool
}

type mockOriginProxy struct {
}

func (moc *mockOriginProxy) Proxy(w ResponseWriter, r *http.Request, sourceConnectionType Type) error {
	if sourceConnectionType == TypeWebsocket {
		return wsEndpoint(w, r)
	}
	switch r.URL.Path {
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

type nowriter struct {
	io.Reader
}

func (nowriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("Writer not implemented")
}

func wsEndpoint(w ResponseWriter, r *http.Request) error {
	resp := &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
	}
	_ = w.WriteRespHeaders(resp.StatusCode, resp.Header)
	clientReader := nowriter{r.Body}
	go func() {
		for {
			data, err := wsutil.ReadClientText(clientReader)
			if err != nil {
				return
			}
			if err := wsutil.WriteServerText(w, data); err != nil {
				return
			}
		}
	}()
	<-r.Context().Done()
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
