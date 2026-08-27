package connection

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProtocolSelector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		protocol         string
		expectedProtocol Protocol
		hasFallback      bool
		expectedFallback Protocol
		wantErr          bool
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
			name:             "named tunnel with quic: no fallback",
			protocol:         "quic",
			expectedProtocol: QUIC,
		},
		{
			name:             "named tunnel with auto: quic",
			protocol:         AutoSelectFlag,
			expectedProtocol: QUIC,
			hasFallback:      true,
			expectedFallback: HTTP2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			selector, err := NewProtocolSelector(test.protocol, &log)
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
