package connection

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/edgediscovery"
)

const (
	testNoTTL            = 0
	noWarpRoutingEnabled = false
)

var (
	testNamedTunnelConfig = &NamedTunnelConfig{
		Credentials: Credentials{
			AccountTag: "testAccountTag",
		},
	}
)

func mockFetcher(getError bool, protocolPercent ...edgediscovery.ProtocolPercent) PercentageFetcher {
	return func() (edgediscovery.ProtocolPercents, error) {
		if getError {
			return nil, fmt.Errorf("failed to fetch percentage")
		}
		return protocolPercent, nil
	}
}

type dynamicMockFetcher struct {
	protocolPercents edgediscovery.ProtocolPercents
	err              error
}

func (dmf *dynamicMockFetcher) fetch() PercentageFetcher {
	return func() (edgediscovery.ProtocolPercents, error) {
		return dmf.protocolPercents, dmf.err
	}
}

func TestNewProtocolSelector(t *testing.T) {
	tests := []struct {
		name               string
		protocol           string
		expectedProtocol   Protocol
		hasFallback        bool
		expectedFallback   Protocol
		warpRoutingEnabled bool
		namedTunnelConfig  *NamedTunnelConfig
		fetchFunc          PercentageFetcher
		wantErr            bool
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
			fetchFunc:         func() (edgediscovery.ProtocolPercents, error) { return nil, nil },
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel over http2",
			protocol:          "http2",
			expectedProtocol:  HTTP2,
			hasFallback:       true,
			expectedFallback:  H2mux,
			fetchFunc:         mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel http2 disabled",
			protocol:          "http2",
			expectedProtocol:  H2mux,
			fetchFunc:         mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: -1}),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:             "named tunnel quic disabled",
			protocol:         "quic",
			expectedProtocol: HTTP2,
			// Hasfallback true is because if http2 fails, then we further fallback to h2mux.
			hasFallback:       true,
			expectedFallback:  H2mux,
			fetchFunc:         mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}, edgediscovery.ProtocolPercent{Protocol: "quic", Percentage: -1}),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel auto all http2 disabled",
			protocol:          "auto",
			expectedProtocol:  H2mux,
			fetchFunc:         mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: -1}),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel auto to h2mux",
			protocol:          "auto",
			expectedProtocol:  H2mux,
			fetchFunc:         mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel auto to http2",
			protocol:          "auto",
			expectedProtocol:  HTTP2,
			hasFallback:       true,
			expectedFallback:  H2mux,
			fetchFunc:         mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:              "named tunnel auto to quic",
			protocol:          "auto",
			expectedProtocol:  QUIC,
			hasFallback:       true,
			expectedFallback:  HTTP2,
			fetchFunc:         mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "quic", Percentage: 100}),
			namedTunnelConfig: testNamedTunnelConfig,
		},
		{
			name:               "warp routing requesting h2mux",
			protocol:           "h2mux",
			expectedProtocol:   HTTP2Warp,
			hasFallback:        false,
			fetchFunc:          mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}),
			warpRoutingEnabled: true,
			namedTunnelConfig:  testNamedTunnelConfig,
		},
		{
			name:               "warp routing requesting h2mux picks HTTP2 even if http2 percent is -1",
			protocol:           "h2mux",
			expectedProtocol:   HTTP2Warp,
			hasFallback:        false,
			fetchFunc:          mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: -1}),
			warpRoutingEnabled: true,
			namedTunnelConfig:  testNamedTunnelConfig,
		},
		{
			name:               "warp routing http2",
			protocol:           "http2",
			expectedProtocol:   HTTP2Warp,
			hasFallback:        false,
			fetchFunc:          mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}),
			warpRoutingEnabled: true,
			namedTunnelConfig:  testNamedTunnelConfig,
		},
		{
			name:               "warp routing quic",
			protocol:           "quic",
			expectedProtocol:   QUICWarp,
			hasFallback:        true,
			expectedFallback:   HTTP2Warp,
			fetchFunc:          mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "quic", Percentage: 100}),
			warpRoutingEnabled: true,
			namedTunnelConfig:  testNamedTunnelConfig,
		},
		{
			name:               "warp routing auto",
			protocol:           "auto",
			expectedProtocol:   HTTP2Warp,
			hasFallback:        false,
			fetchFunc:          mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}),
			warpRoutingEnabled: true,
			namedTunnelConfig:  testNamedTunnelConfig,
		},
		{
			name:               "warp routing auto- quic",
			protocol:           "auto",
			expectedProtocol:   QUICWarp,
			hasFallback:        true,
			expectedFallback:   HTTP2Warp,
			fetchFunc:          mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}, edgediscovery.ProtocolPercent{Protocol: "quic", Percentage: 100}),
			warpRoutingEnabled: true,
			namedTunnelConfig:  testNamedTunnelConfig,
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
			fetchFunc:         mockFetcher(false, edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}),
			namedTunnelConfig: testNamedTunnelConfig,
			wantErr:           true,
		},
		{
			name:              "named tunnel fetch error",
			protocol:          "auto",
			fetchFunc:         mockFetcher(true),
			namedTunnelConfig: testNamedTunnelConfig,
			expectedProtocol:  HTTP2,
			wantErr:           false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selector, err := NewProtocolSelector(test.protocol, test.warpRoutingEnabled, test.namedTunnelConfig, test.fetchFunc, testNoTTL, &log)
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
		})
	}
}

func TestAutoProtocolSelectorRefresh(t *testing.T) {
	fetcher := dynamicMockFetcher{}
	selector, err := NewProtocolSelector("auto", noWarpRoutingEnabled, testNamedTunnelConfig, fetcher.fetch(), testNoTTL, &log)
	assert.NoError(t, err)
	assert.Equal(t, H2mux, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}}
	assert.Equal(t, H2mux, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.err = fmt.Errorf("failed to fetch")
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: -1}}
	fetcher.err = nil
	assert.Equal(t, H2mux, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}}
	assert.Equal(t, H2mux, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "quic", Percentage: 100}}
	assert.Equal(t, QUIC, selector.Current())
}

func TestHTTP2ProtocolSelectorRefresh(t *testing.T) {
	fetcher := dynamicMockFetcher{}
	selector, err := NewProtocolSelector("http2", noWarpRoutingEnabled, testNamedTunnelConfig, fetcher.fetch(), testNoTTL, &log)
	assert.NoError(t, err)
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.err = fmt.Errorf("failed to fetch")
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: -1}}
	fetcher.err = nil
	assert.Equal(t, H2mux, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: -1}}
	assert.Equal(t, H2mux, selector.Current())
}

func TestProtocolSelectorRefreshTTL(t *testing.T) {
	fetcher := dynamicMockFetcher{}
	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "quic", Percentage: 100}}
	selector, err := NewProtocolSelector("auto", noWarpRoutingEnabled, testNamedTunnelConfig, fetcher.fetch(), time.Hour, &log)
	assert.NoError(t, err)
	assert.Equal(t, QUIC, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "quic", Percentage: 0}}
	assert.Equal(t, QUIC, selector.Current())
}
