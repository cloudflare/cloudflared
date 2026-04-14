package connection

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/edgediscovery"
)

const (
	testNoTTL      = 0
	testAccountTag = "testAccountTag"
)

type dynamicMockFetcher struct {
	protocolPercents edgediscovery.ProtocolPercents
	err              error
}

func (dmf *dynamicMockFetcher) fetch() edgediscovery.PercentageFetcher {
	return func() (edgediscovery.ProtocolPercents, error) {
		return dmf.protocolPercents, dmf.err
	}
}

func TestNewProtocolSelector(t *testing.T) {
	tests := []struct {
		name                string
		protocol            string
		tunnelTokenProvided bool
		needPQ              bool
		expectedProtocol    Protocol
		hasFallback         bool
		expectedFallback    Protocol
		wantErr             bool
	}{
		{
			name:     "named tunnel with unknown protocol",
			protocol: "unknown",
			wantErr:  true,
		},
		{
			name:             "named tunnel with h2mux: force to http2",
			protocol:         "h2mux",
			expectedProtocol: HTTP2,
		},
		{
			name:             "named tunnel with http2: no fallback",
			protocol:         "http2",
			expectedProtocol: HTTP2,
		},
		{
			name:             "named tunnel with auto: quic",
			protocol:         AutoSelectFlag,
			expectedProtocol: QUIC,
			hasFallback:      true,
			expectedFallback: HTTP2,
		},
		{
			name:             "named tunnel (post quantum)",
			protocol:         AutoSelectFlag,
			needPQ:           true,
			expectedProtocol: QUIC,
		},
		{
			name:             "named tunnel (post quantum) w/http2",
			protocol:         "http2",
			needPQ:           true,
			expectedProtocol: QUIC,
		},
	}

	fetcher := dynamicMockFetcher{
		protocolPercents: edgediscovery.ProtocolPercents{},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selector, err := NewProtocolSelector(test.protocol, testAccountTag, test.tunnelTokenProvided, test.needPQ, fetcher.fetch(), ResolveTTL, &log)
			if test.wantErr {
				assert.Error(t, err, "test %s failed", test.name)
			} else {
				require.NoError(t, err, "test %s failed", test.name)
				assert.Equalf(t, test.expectedProtocol, selector.Current(), "test %s failed", test.name)
				fallback, ok := selector.Fallback()
				assert.Equalf(t, test.hasFallback, ok, "test %s failed", test.name)
				if test.hasFallback {
					assert.Equalf(t, test.expectedFallback, fallback, "test %s failed", test.name)
				}
			}
		})
	}
}

func TestAutoProtocolSelectorRefresh(t *testing.T) {
	fetcher := dynamicMockFetcher{}
	selector, err := NewProtocolSelector(AutoSelectFlag, testAccountTag, false, false, fetcher.fetch(), testNoTTL, &log)
	require.NoError(t, err)
	assert.Equal(t, QUIC, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}}
	assert.Equal(t, QUIC, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.err = fmt.Errorf("failed to fetch")
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: -1}}
	fetcher.err = nil
	assert.Equal(t, QUIC, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}}
	assert.Equal(t, QUIC, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "quic", Percentage: 100}}
	assert.Equal(t, QUIC, selector.Current())
}

func TestHTTP2ProtocolSelectorRefresh(t *testing.T) {
	fetcher := dynamicMockFetcher{}
	// Since the user chooses http2 on purpose, we always stick to it.
	selector, err := NewProtocolSelector(HTTP2.String(), testAccountTag, false, false, fetcher.fetch(), testNoTTL, &log)
	require.NoError(t, err)
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.err = fmt.Errorf("failed to fetch")
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: -1}}
	fetcher.err = nil
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 0}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}}
	assert.Equal(t, HTTP2, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: -1}}
	assert.Equal(t, HTTP2, selector.Current())
}

func TestAutoProtocolSelectorNoRefreshWithToken(t *testing.T) {
	fetcher := dynamicMockFetcher{}
	selector, err := NewProtocolSelector(AutoSelectFlag, testAccountTag, true, false, fetcher.fetch(), testNoTTL, &log)
	require.NoError(t, err)
	assert.Equal(t, QUIC, selector.Current())

	fetcher.protocolPercents = edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}}
	assert.Equal(t, QUIC, selector.Current())
}

func TestProbeTLSSettings(t *testing.T) {
	tests := []struct {
		name           string
		protocol       Protocol
		expectedServer string
		expectedProtos []string
		expectNil      bool
	}{
		{
			name:           "HTTP2 returns probe SNI",
			protocol:       HTTP2,
			expectedServer: probeTLSServerName,
			expectedProtos: nil,
		},
		{
			name:           "QUIC returns probe SNI with alpn",
			protocol:       QUIC,
			expectedServer: probeTLSServerName,
			expectedProtos: []string{"argotunnel"},
		},
		{
			name:      "Unknown protocol returns nil",
			protocol:  Protocol(999),
			expectNil: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := test.protocol.ProbeTLSSettings()
			if test.expectNil {
				assert.Nil(t, settings)
			} else {
				assert.NotNil(t, settings)
				assert.Equal(t, test.expectedServer, settings.ServerName)
				assert.Equal(t, test.expectedProtos, settings.NextProtos)
			}
		})
	}
}
