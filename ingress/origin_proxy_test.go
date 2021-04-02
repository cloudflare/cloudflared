package ingress

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/websocket"
)

// TestEstablishConnectionResponse ensures each implementation of StreamBasedOriginProxy returns
// the expected response
func assertEstablishConnectionResponse(t *testing.T,
	originProxy StreamBasedOriginProxy,
	req *http.Request,
	expectHeader http.Header,
) {
	_, resp, err := originProxy.EstablishConnection(req)
	assert.NoError(t, err)
	assert.Equal(t, switchingProtocolText, resp.Status)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	assert.Equal(t, expectHeader, resp.Header)
}

func TestHTTPServiceEstablishConnection(t *testing.T) {
	origin := echoWSOrigin(t, false)
	defer origin.Close()
	originURL, err := url.Parse(origin.URL)
	require.NoError(t, err)

	httpService := &httpService{
		url:        originURL,
		hostHeader: origin.URL,
		transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Test-Cloudflared-Echo", t.Name())

	expectHeader := http.Header{
		"Connection":            {"Upgrade"},
		"Sec-Websocket-Accept":  {"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
		"Upgrade":               {"websocket"},
		"Test-Cloudflared-Echo": {t.Name()},
	}
	assertEstablishConnectionResponse(t, httpService, req, expectHeader)
}

func TestHelloWorldEstablishConnection(t *testing.T) {
	var wg sync.WaitGroup
	shutdownC := make(chan struct{})
	errC := make(chan error)
	helloWorldSerivce := &helloWorld{}
	helloWorldSerivce.start(&wg, testLogger, shutdownC, errC, OriginRequestConfig{})

	// Scheme and Host of URL will be override by the Scheme and Host of the helloWorld service
	req, err := http.NewRequest(http.MethodGet, "https://place-holder/ws", nil)
	require.NoError(t, err)
	req.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	expectHeader := http.Header{
		"Connection":           {"Upgrade"},
		"Sec-Websocket-Accept": {"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
		"Upgrade":              {"websocket"},
	}
	assertEstablishConnectionResponse(t, helloWorldSerivce, req, expectHeader)

	close(shutdownC)
}

func TestRawTCPServiceEstablishConnection(t *testing.T) {
	originListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	listenerClosed := make(chan struct{})
	tcpListenRoutine(originListener, listenerClosed)

	rawTCPService := &rawTCPService{name: ServiceWarpRouting}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", originListener.Addr()), nil)
	require.NoError(t, err)

	assertEstablishConnectionResponse(t, rawTCPService, req, nil)

	originListener.Close()
	<-listenerClosed

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", originListener.Addr()), nil)
	require.NoError(t, err)

	// Origin not listening for new connection, should return an error
	_, resp, err := rawTCPService.EstablishConnection(req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestTCPOverWSServiceEstablishConnection(t *testing.T) {
	originListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	listenerClosed := make(chan struct{})
	tcpListenRoutine(originListener, listenerClosed)

	originURL := &url.URL{
		Scheme: "tcp",
		Host:   originListener.Addr().String(),
	}

	baseReq, err := http.NewRequest(http.MethodGet, "https://place-holder", nil)
	require.NoError(t, err)
	baseReq.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	bastionReq := baseReq.Clone(context.Background())
	carrier.SetBastionDest(bastionReq.Header, originListener.Addr().String())

	expectHeader := http.Header{
		"Connection":           {"Upgrade"},
		"Sec-Websocket-Accept": {"s3pPLMBiTxaQ9kYGzzhZRbK+xOo="},
		"Upgrade":              {"websocket"},
	}

	tests := []struct {
		testCase  string
		service   *tcpOverWSService
		req       *http.Request
		expectErr bool
	}{
		{
			testCase: "specific TCP service",
			service:  newTCPOverWSService(originURL),
			req:      baseReq,
		},
		{
			testCase: "bastion service",
			service:  newBastionService(),
			req:      bastionReq,
		},
		{
			testCase:  "invalid bastion request",
			service:   newBastionService(),
			req:       baseReq,
			expectErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.testCase, func(t *testing.T) {
			if test.expectErr {
				_, resp, err := test.service.EstablishConnection(test.req)
				assert.Error(t, err)
				assert.Nil(t, resp)
			} else {
				assertEstablishConnectionResponse(t, test.service, test.req, expectHeader)
			}
		})
	}

	originListener.Close()
	<-listenerClosed

	for _, service := range []*tcpOverWSService{newTCPOverWSService(originURL), newBastionService()} {
		// Origin not listening for new connection, should return an error
		_, resp, err := service.EstablishConnection(bastionReq)
		assert.Error(t, err)
		assert.Nil(t, resp)
	}
}

func TestHTTPServiceHostHeaderOverride(t *testing.T) {
	cfg := OriginRequestConfig{
		HTTPHostHeader: t.Name(),
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, r.Host, t.Name())
		if websocket.IsWebSocketUpgrade(r) {
			respHeaders := websocket.NewResponseHeader(r)
			for k, v := range respHeaders {
				w.Header().Set(k, v[0])
			}
			w.WriteHeader(http.StatusSwitchingProtocols)
			return
		}
		w.Write([]byte("ok"))
	}
	origin := httptest.NewServer(http.HandlerFunc(handler))
	defer origin.Close()

	originURL, err := url.Parse(origin.URL)
	require.NoError(t, err)

	httpService := &httpService{
		url: originURL,
	}
	var wg sync.WaitGroup
	shutdownC := make(chan struct{})
	errC := make(chan error)
	require.NoError(t, httpService.start(&wg, testLogger, shutdownC, errC, cfg))

	req, err := http.NewRequest(http.MethodGet, originURL.String(), nil)
	require.NoError(t, err)

	resp, err := httpService.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	req = req.Clone(context.Background())
	_, resp, err = httpService.EstablishConnection(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
}

func tcpListenRoutine(listener net.Listener, closeChan chan struct{}) {
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				close(closeChan)
				return
			}
			// Close immediately, this test is not about testing read/write on connection
			conn.Close()
		}
	}()
}
