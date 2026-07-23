package ingress

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

const testOriginProtocolHeader = "X-Test-Origin-Protocol"

func TestAddPortIfMissing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input    string
		expected string
	}{
		{"ssh://[::1]", "[::1]:22"},
		{"ssh://[::1]:38", "[::1]:38"},
		{"ssh://abc:38", "abc:38"},
		{"ssh://127.0.0.1:38", "127.0.0.1:38"},
		{"ssh://127.0.0.1", "127.0.0.1:22"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			url1, err := url.Parse(tc.input)
			require.NoError(t, err)
			addPortIfMissing(url1, 22)
			require.Equal(t, tc.expected, url1.Host)
		})
	}
}

func TestIsH2COrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		service  OriginService
		expected bool
	}{
		{
			name:     "HTTP",
			service:  &httpService{url: MustParseURL(t, "http://localhost")},
			expected: true,
		},
		{
			name:    "HTTPS",
			service: &httpService{url: MustParseURL(t, "https://localhost")},
		},
		{
			name:    "WebSocket",
			service: &httpService{url: MustParseURL(t, "ws://localhost")},
		},
		{
			name:    "secure WebSocket",
			service: &httpService{url: MustParseURL(t, "wss://localhost")},
		},
		{
			name:     "Unix",
			service:  &unixSocketPath{path: "/tmp/cloudflared.sock", scheme: "http"},
			expected: true,
		},
		{
			name:    "Unix with TLS",
			service: &unixSocketPath{path: "/tmp/cloudflared.sock", scheme: "https"},
		},
		{
			name:    "HTTP service without URL",
			service: &httpService{},
		},
		{
			name:    "Hello World before configuration",
			service: &helloWorld{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.expected, isH2COrigin(test.service))
		})
	}
}

func TestHTTPTransportUsesH2C(t *testing.T) {
	t.Parallel()

	origin := newTestOriginServer(t, unencryptedHTTP2Protocols(), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(testOriginProtocolHeader, r.Proto)
		w.WriteHeader(http.StatusNoContent)
	})
	service := newTestHTTPService(t, origin.URL, OriginRequestConfig{Http2Origin: true})

	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	require.NoError(t, err)
	resp, err := service.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "HTTP/2.0", resp.Header.Get(testOriginProtocolHeader))
	require.Equal(t, 2, resp.ProtoMajor)
	require.NoError(t, resp.Body.Close())
}

func TestHTTPTransportUsesHTTP1WhenHTTP2OriginIsDisabled(t *testing.T) {
	t.Parallel()

	origin := newTestOriginServer(t, http1AndUnencryptedHTTP2Protocols(), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(testOriginProtocolHeader, r.Proto)
		w.WriteHeader(http.StatusNoContent)
	})
	service := newTestHTTPService(t, origin.URL, OriginRequestConfig{})

	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	require.NoError(t, err)
	resp, err := service.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "HTTP/1.1", resp.Header.Get(testOriginProtocolHeader))
	require.Equal(t, 1, resp.ProtoMajor)
	require.NoError(t, resp.Body.Close())
}

func TestHTTPTransportDoesNotFallBackFromH2CToHTTP1(t *testing.T) {
	t.Parallel()

	origin := newTestOriginServer(t, http1Protocols(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	service := newTestHTTPService(t, origin.URL, OriginRequestConfig{Http2Origin: true})

	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	require.NoError(t, err)
	resp, err := service.RoundTrip(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestHTTPTransportUsesHTTP2OverTLS(t *testing.T) {
	t.Parallel()

	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(testOriginProtocolHeader, r.Proto)
		w.WriteHeader(http.StatusNoContent)
	}))
	origin.EnableHTTP2 = true
	origin.StartTLS()
	t.Cleanup(origin.Close)
	service := newTestHTTPService(t, origin.URL, OriginRequestConfig{
		Http2Origin: true,
		NoTLSVerify: true,
	})

	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	require.NoError(t, err)
	resp, err := service.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "HTTP/2.0", resp.Header.Get(testOriginProtocolHeader))
	require.Equal(t, 2, resp.ProtoMajor)
	require.NoError(t, resp.Body.Close())
}

func TestWebSocketOriginUsesHTTP1WithHTTP2OriginEnabled(t *testing.T) {
	t.Parallel()

	origin := newTestOriginServer(t, http1AndUnencryptedHTTP2Protocols(), func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(testOriginProtocolHeader, r.Proto)
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Upgrade", "websocket")
		w.WriteHeader(http.StatusSwitchingProtocols)
	})
	originURL := MustParseURL(t, origin.URL)
	originURL.Scheme = "ws"
	service := newTestHTTPService(t, originURL.String(), OriginRequestConfig{Http2Origin: true})

	req, err := http.NewRequest(http.MethodGet, "http://eyeball.example", nil)
	require.NoError(t, err)
	setWebSocketUpgradeHeaders(req)
	resp, err := service.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	require.Equal(t, "HTTP/1.1", resp.Header.Get(testOriginProtocolHeader))
	require.Equal(t, 1, resp.ProtoMajor)
	require.NoError(t, resp.Body.Close())
}

func TestWebSocketUpgradeDoesNotFallBackFromH2C(t *testing.T) {
	t.Parallel()

	origin := newTestOriginServer(t, http1AndUnencryptedHTTP2Protocols(), func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Connection", "Upgrade")
		w.Header().Set("Upgrade", "websocket")
		w.WriteHeader(http.StatusSwitchingProtocols)
	})
	service := newTestHTTPService(t, origin.URL, OriginRequestConfig{Http2Origin: true})

	req, err := http.NewRequest(http.MethodGet, "http://eyeball.example", nil)
	require.NoError(t, err)
	setWebSocketUpgradeHeaders(req)
	resp, err := service.RoundTrip(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.Error(t, err)
	require.Nil(t, resp)
}

func newTestHTTPService(t *testing.T, rawURL string, cfg OriginRequestConfig) *httpService {
	t.Helper()

	service := &httpService{url: MustParseURL(t, rawURL)}
	require.NoError(t, service.start(TestLogger, nil, cfg))
	service.transport.Proxy = nil
	t.Cleanup(service.transport.CloseIdleConnections)
	return service
}

func newTestOriginServer(t *testing.T, protocols *http.Protocols, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	origin := httptest.NewUnstartedServer(handler)
	origin.Config.Protocols = protocols
	origin.Start()
	t.Cleanup(origin.Close)
	return origin
}

func http1Protocols() *http.Protocols {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	return protocols
}

func unencryptedHTTP2Protocols() *http.Protocols {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	return protocols
}

func http1AndUnencryptedHTTP2Protocols() *http.Protocols {
	protocols := http1Protocols()
	protocols.SetUnencryptedHTTP2(true)
	return protocols
}

func setWebSocketUpgradeHeaders(req *http.Request) {
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
}
