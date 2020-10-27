package connection

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/ui"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/gobwas/ws/wsutil"
)

const (
	largeFileSize = 2 * 1024 * 1024
)

var (
	testConfig = &Config{
		OriginClient: &mockOriginClient{},
		GracePeriod:  time.Millisecond * 100,
	}
	testLogger, _ = logger.New()
	testOriginURL = &url.URL{
		Scheme: "https",
		Host:   "connectiontest.argotunnel.com",
	}
	testTunnelEventChan = make(chan ui.TunnelEvent)
	testObserver        = &Observer{
		testLogger,
		m,
		testTunnelEventChan,
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

type mockOriginClient struct {
}

func (moc *mockOriginClient) Proxy(w ResponseWriter, r *http.Request, isWebsocket bool) error {
	if isWebsocket {
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
	w.WriteRespHeaders(resp)
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
	w.WriteRespHeaders(resp)
	w.Write(data)
}

type mockConnectedFuse struct{}

func (mcf mockConnectedFuse) Connected() {}

func (mcf mockConnectedFuse) IsConnected() bool {
	return true
}
