package connection

import (
	"fmt"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/stretchr/testify/assert"
)

const (
	testNoTTL = 0
)

var (
	testNamedTunnelConfig = &NamedTunnelConfig{
		Auth: pogs.TunnelAuth{
			AccountTag: "testAccountTag",
		},
	}
)

func mockFetcher(percentage int32) PercentageFetcher {
	return func() (int32, error) {
		return percentage, nil
	}
}

func mockFetcherWithError() PercentageFetcher {
	return func() (int32, error) {
		return 0, fmt.Errorf("failed to fetch precentage")
	}
}

type dynamicMockFetcher struct {
	percentage int32
	err        error
}

func (dmf *dynamicMockFetcher) fetch() PercentageFetcher {
	return func() (int32, error) {
		if dmf.err != nil {
			return 0, dmf.err
		}
		return dmf.percentage, nil
	}
}

func TestNewProtocolSelector(t *testing.T) {
	tests := []struct {
		name              string
		protocol          string
		expectedProtocol  Protocol
		hasFallback       bool
		expectedFallback  Protocol
		namedTunnelConfig *NamedTunnelConfig
		fetchFunc         PercentageFetcher
		wantErr           bool
	}{
		{
			name:              "classic tunnel",
			protocol:          "h2mux",
			expectedProtocol:  H2mux,
			namedTunnelConfig: nil,
		},
		{
			name:              "named tunnel over h2mux",
			protocol:          "h2mux",
			expectedProtocol:  H2mux,
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel over http2",
			protocol:          "http2",
			expectedProtocol:  HTTP2,
			hasFallback:       true,
			expectedFallback:  H2mux,
			fetchFunc:         mockFetcher(0),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel http2 disabled",
			protocol:          "http2",
			expectedProtocol:  H2mux,
			fetchFunc:         mockFetcher(-1),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel auto all http2 disabled",
			protocol:          "auto",
			expectedProtocol:  H2mux,
			fetchFunc:         mockFetcher(-1),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel auto to h2mux",
			protocol:          "auto",
			expectedProtocol:  H2mux,
			fetchFunc:         mockFetcher(0),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel auto to http2",
			protocol:          "auto",
			expectedProtocol:  HTTP2,
			hasFallback:       true,
			expectedFallback:  H2mux,
			fetchFunc:         mockFetcher(100),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			// None named tunnel can only use h2mux, so specifying an unknown protocol is not an error
			name:             "classic tunnel unknown protocol",
			protocol:         "unknown",
			expectedProtocol: H2mux,
		},
		{
			name:              "named tunnel unknown protocol",
			protocol:          "unknown",
			fetchFunc:         mockFetcher(100),
			namedTunnelConfig: testNamedTunnelConfig,
			wantErr:           true,
		},
		{
			name:              "named tunnel fetch error",
			protocol:          "unknown",
			fetchFunc:         mockFetcherWithError(),
			namedTunnelConfig: testNamedTunnelConfig,
			wantErr:           true,
		},
	}
	logger, _ := logger.New()
	for _, test := range tests {
		selector, err := NewProtocolSelector(test.protocol, test.namedTunnelConfig, test.fetchFunc, testNoTTL, logger)
		if test.wantErr {
			assert.Error(t, err, fmt.Sprintf("test %s failed", test.name))
		} else {
			assert.NoError(t, err, fmt.Sprintf("test %s failed", test.name))
			assert.Equal(t, test.expectedProtocol, selector.Current(), fmt.Sprintf("test %s failed", test.name))
			fallback, ok := selector.Fallback()
			assert.Equal(t, test.hasFallback, ok, fmt.Sprintf("test %s failed", test.name))
			if test.hasFallback {
				assert.Equal(t, test.expectedFallback, fallback, fmt.Sprintf("test %s failed", test.name))
			}
		}
	}
}

func TestAutoProtocolSelectorRefresh(t *testing.T) {
	logger, _ := logger.New()
	fetcher := dynamicMockFetcher{}
	selector, err := NewProtocolSelector("auto", testNamedTunnelConfig, fetcher.fetch(), testNoTTL, logger)
	assert.NoError(t, err)
	assert.Equal(t, H2mux, selector.Current())

	fetcher.percentage = 100
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.percentage = 0
	assert.Equal(t, H2mux, selector.Current())

	fetcher.percentage = 100
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.err = fmt.Errorf("failed to fetch")
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.percentage = -1
	fetcher.err = nil
	assert.Equal(t, H2mux, selector.Current())

	fetcher.percentage = 0
	assert.Equal(t, H2mux, selector.Current())

	fetcher.percentage = 100
	assert.Equal(t, HTTP2, selector.Current())
}

func TestHTTP2ProtocolSelectorRefresh(t *testing.T) {
	logger, _ := logger.New()
	fetcher := dynamicMockFetcher{}
	selector, err := NewProtocolSelector("http2", testNamedTunnelConfig, fetcher.fetch(), testNoTTL, logger)
	assert.NoError(t, err)
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.percentage = 100
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.percentage = 0
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.err = fmt.Errorf("failed to fetch")
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.percentage = -1
	fetcher.err = nil
	assert.Equal(t, H2mux, selector.Current())

	fetcher.percentage = 0
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.percentage = 100
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.percentage = -1
	assert.Equal(t, H2mux, selector.Current())
}

func TestProtocolSelectorRefreshTTL(t *testing.T) {
	logger, _ := logger.New()
	fetcher := dynamicMockFetcher{percentage: 100}
	selector, err := NewProtocolSelector("auto", testNamedTunnelConfig, fetcher.fetch(), time.Hour, logger)
	assert.NoError(t, err)
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.percentage = 0
	assert.Equal(t, HTTP2, selector.Current())
}
