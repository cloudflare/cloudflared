package ingress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBridgeServiceDestination(t *testing.T) {
	canonicalJumpDestHeader := http.CanonicalHeaderKey(h2mux.CFJumpDestinationHeader)
	tests := []struct {
		name         string
		header       http.Header
		expectedDest string
		wantErr      bool
	}{
		{
			name: "hostname destination",
			header: http.Header{
				canonicalJumpDestHeader: []string{"localhost"},
			},
			expectedDest: "localhost",
		},
		{
			name: "hostname destination with port",
			header: http.Header{
				canonicalJumpDestHeader: []string{"localhost:9000"},
			},
			expectedDest: "localhost:9000",
		},
		{
			name: "hostname destination with scheme and port",
			header: http.Header{
				canonicalJumpDestHeader: []string{"ssh://localhost:9000"},
			},
			expectedDest: "localhost:9000",
		},
		{
			name: "full hostname url",
			header: http.Header{
				canonicalJumpDestHeader: []string{"ssh://localhost:9000/metrics"},
			},
			expectedDest: "localhost:9000",
		},
		{
			name: "hostname destination with port and path",
			header: http.Header{
				canonicalJumpDestHeader: []string{"localhost:9000/metrics"},
			},
			expectedDest: "localhost:9000",
		},
		{
			name: "ip destination",
			header: http.Header{
				canonicalJumpDestHeader: []string{"127.0.0.1"},
			},
			expectedDest: "127.0.0.1",
		},
		{
			name: "ip destination with port",
			header: http.Header{
				canonicalJumpDestHeader: []string{"127.0.0.1:9000"},
			},
			expectedDest: "127.0.0.1:9000",
		},
		{
			name: "ip destination with port and path",
			header: http.Header{
				canonicalJumpDestHeader: []string{"127.0.0.1:9000/metrics"},
			},
			expectedDest: "127.0.0.1:9000",
		},
		{
			name: "ip destination with schem and port",
			header: http.Header{
				canonicalJumpDestHeader: []string{"tcp://127.0.0.1:9000"},
			},
			expectedDest: "127.0.0.1:9000",
		},
		{
			name: "full ip url",
			header: http.Header{
				canonicalJumpDestHeader: []string{"ssh://127.0.0.1:9000/metrics"},
			},
			expectedDest: "127.0.0.1:9000",
		},
		{
			name:    "no destination",
			wantErr: true,
		},
	}
	s := newBridgeService(nil, ServiceBastion)
	for _, test := range tests {
		r := &http.Request{
			Header: test.header,
		}
		dest, err := s.destination(r)
		if test.wantErr {
			assert.Error(t, err, "Test %s expects error", test.name)
		} else {
			assert.NoError(t, err, "Test %s expects no error, got error %v", test.name, err)
			assert.Equal(t, test.expectedDest, dest, "Test %s expect dest %s, got %s", test.name, test.expectedDest, dest)
		}
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
	log := zerolog.Nop()
	shutdownC := make(chan struct{})
	errC := make(chan error)
	require.NoError(t, httpService.start(&wg, &log, shutdownC, errC, cfg))

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
