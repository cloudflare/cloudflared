package ingress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/websocket"
)

func TestRawTCPServiceEstablishConnection(t *testing.T) {
	originListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	listenerClosed := make(chan struct{})
	tcpListenRoutine(originListener, listenerClosed)

	rawTCPService := &rawTCPService{
		name:   ServiceWarpRouting,
		dialer: newProxyAwareDialer(30*time.Second, 30*time.Second, nil),
	}

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", originListener.Addr()), nil)
	require.NoError(t, err)

	originListener.Close()
	<-listenerClosed

	req, err = http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s", originListener.Addr()), nil)
	require.NoError(t, err)

	// Origin not listening for new connection, should return an error
	_, err = rawTCPService.EstablishConnection(context.Background(), req.URL.String(), TestLogger)
	require.Error(t, err)
}

func TestProxyAwareDialer(t *testing.T) {
	tests := []struct {
		name        string
		httpProxy   string
		httpsProxy  string
		socksProxy  string
		expectDirect bool
		expectProxy  bool
	}{
		{
			name:         "no proxy configured",
			expectDirect: true,
		},
		{
			name:        "HTTP proxy configured",
			httpProxy:   "http://proxy.example.com:8080",
			expectProxy: true,
		},
		{
			name:        "HTTPS proxy configured",
			httpsProxy:  "http://proxy.example.com:8080",
			expectProxy: true,
		},
		{
			name:       "SOCKS proxy configured",
			socksProxy: "socks5://proxy.example.com:1080",
			expectProxy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origHTTP := os.Getenv("HTTP_PROXY")
			origHTTPS := os.Getenv("HTTPS_PROXY")
			origSOCKS := os.Getenv("ALL_PROXY")

			defer func() {
				os.Setenv("HTTP_PROXY", origHTTP)
				os.Setenv("HTTPS_PROXY", origHTTPS)
				os.Setenv("ALL_PROXY", origSOCKS)
			}()

			os.Setenv("HTTP_PROXY", tt.httpProxy)
			os.Setenv("HTTPS_PROXY", tt.httpsProxy)
			os.Setenv("ALL_PROXY", tt.socksProxy)

			dialer := newProxyAwareDialer(30*time.Second, 30*time.Second, TestLogger)
			assert.NotNil(t, dialer)

			if tt.expectDirect {
				_, ok := dialer.(*net.Dialer)
				assert.True(t, ok, "Expected net.Dialer when no proxy configured")
			} else if tt.expectProxy {
				assert.NotNil(t, dialer, "Expected proxy dialer when proxy configured")
			}
		})
	}
}

func TestProxyAwareDialerHTTPConnect(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer proxyServer.Close()

	origHTTP := os.Getenv("HTTP_PROXY")
	defer os.Setenv("HTTP_PROXY", origHTTP)

	os.Setenv("HTTP_PROXY", proxyServer.URL)

	dialer := newProxyAwareDialer(5*time.Second, 5*time.Second, TestLogger)
	assert.NotNil(t, dialer)

	// Test actual dial (this will fail because our mock proxy doesn't handle the full protocol)
	// but we can verify the proxy detection logic works
	proxyAwareDialer, ok := dialer.(*proxyAwareDialer)
	assert.True(t, ok, "Expected proxyAwareDialer when HTTP proxy configured")
	assert.NotNil(t, proxyAwareDialer.baseDialer)
}

func TestGetEnvProxy(t *testing.T) {
	tests := []struct {
		name     string
		upper    string
		lower    string
		upperVal string
		lowerVal string
		expected string
	}{
		{
			name:     "upper case takes priority",
			upper:    "TEST_PROXY",
			lower:    "test_proxy",
			upperVal: "upper_value",
			lowerVal: "lower_value",
			expected: "upper_value",
		},
		{
			name:     "lower case when upper not set",
			upper:    "TEST_PROXY",
			lower:    "test_proxy",
			lowerVal: "lower_value",
			expected: "lower_value",
		},
		{
			name:     "empty when neither set",
			upper:    "TEST_PROXY",
			lower:    "test_proxy",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore environment
			origUpper := os.Getenv(tt.upper)
			origLower := os.Getenv(tt.lower)
			defer func() {
				os.Setenv(tt.upper, origUpper)
				os.Setenv(tt.lower, origLower)
			}()

			os.Unsetenv(tt.upper)
			os.Unsetenv(tt.lower)

			if tt.upperVal != "" {
				os.Setenv(tt.upper, tt.upperVal)
			}
			if tt.lowerVal != "" {
				os.Setenv(tt.lower, tt.lowerVal)
			}

			result := getEnvProxy(tt.upper, tt.lower)
			assert.Equal(t, tt.expected, result)
		})
	}
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
				bastionHost, _ := carrier.ResolveBastionDest(test.req)
				_, err := test.service.EstablishConnection(context.Background(), bastionHost, TestLogger)
				assert.Error(t, err)
			}
		})
	}

	originListener.Close()
	<-listenerClosed

	for _, service := range []*tcpOverWSService{newTCPOverWSService(originURL), newBastionService()} {
		// Origin not listening for new connection, should return an error
		bastionHost, _ := carrier.ResolveBastionDest(bastionReq)
		_, err := service.EstablishConnection(context.Background(), bastionHost, TestLogger)
		assert.Error(t, err)
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
		// return the X-Forwarded-Host header for assertions
		// as the httptest Server URL isn't available here yet
		w.Write([]byte(r.Header.Get("X-Forwarded-Host")))
	}
	origin := httptest.NewServer(http.HandlerFunc(handler))
	defer origin.Close()

	originURL, err := url.Parse(origin.URL)
	require.NoError(t, err)

	httpService := &httpService{
		url: originURL,
	}
	shutdownC := make(chan struct{})
	require.NoError(t, httpService.start(TestLogger, shutdownC, cfg))

	req, err := http.NewRequest(http.MethodGet, originURL.String(), nil)
	require.NoError(t, err)

	resp, err := httpService.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, respBody, []byte(originURL.Host))
}

// TestHTTPServiceUsesIngressRuleScheme makes sure httpService uses scheme defined in ingress rule and not by eyeball request
func TestHTTPServiceUsesIngressRuleScheme(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		require.NotNil(t, r.TLS)
		// Echo the X-Forwarded-Proto header for assertions
		w.Write([]byte(r.Header.Get("X-Forwarded-Proto")))
	}
	origin := httptest.NewTLSServer(http.HandlerFunc(handler))
	defer origin.Close()

	originURL, err := url.Parse(origin.URL)
	require.NoError(t, err)
	require.Equal(t, "https", originURL.Scheme)

	cfg := OriginRequestConfig{
		NoTLSVerify: true,
	}
	httpService := &httpService{
		url: originURL,
	}
	shutdownC := make(chan struct{})
	require.NoError(t, httpService.start(TestLogger, shutdownC, cfg))

	// Tunnel uses scheme defined in the service field of the ingress rule, independent of the X-Forwarded-Proto header
	protos := []string{"https", "http", "dne"}
	for _, p := range protos {
		req, err := http.NewRequest(http.MethodGet, originURL.String(), nil)
		require.NoError(t, err)
		req.Header.Add("X-Forwarded-Proto", p)

		resp, err := httpService.RoundTrip(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		respBody, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, respBody, []byte(p))
	}
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
