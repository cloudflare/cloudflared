package origin

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/retry"
)

type dynamicMockFetcher struct {
	protocolPercents edgediscovery.ProtocolPercents
	err              error
}

func (dmf *dynamicMockFetcher) fetch() connection.PercentageFetcher {
	return func() (edgediscovery.ProtocolPercents, error) {
		return dmf.protocolPercents, dmf.err
	}
}

func TestWaitForBackoffFallback(t *testing.T) {
	maxRetries := uint(3)
	backoff := retry.BackoffHandler{
		MaxRetries: maxRetries,
		BaseTime:   time.Millisecond * 10,
	}
	log := zerolog.Nop()
	resolveTTL := time.Duration(0)
	namedTunnel := &connection.NamedTunnelConfig{
		Credentials: connection.Credentials{
			AccountTag: "test-account",
		},
	}
	mockFetcher := dynamicMockFetcher{
		protocolPercents: edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "http2", Percentage: 100}},
	}
	warpRoutingEnabled := false
	protocolSelector, err := connection.NewProtocolSelector(
		"auto",
		warpRoutingEnabled,
		namedTunnel,
		mockFetcher.fetch(),
		resolveTTL,
		&log,
	)
	assert.NoError(t, err)

	initProtocol := protocolSelector.Current()
	assert.Equal(t, connection.HTTP2, initProtocol)

	protocolFallback := &protocolFallback{
		backoff,
		initProtocol,
		false,
	}

	// Retry #0 and #1. At retry #2, we switch protocol, so the fallback loop has one more retry than this
	for i := 0; i < int(maxRetries-1); i++ {
		protocolFallback.BackoffTimer() // simulate retry
		ok := selectNextProtocol(&log, protocolFallback, protocolSelector, false)
		assert.True(t, ok)
		assert.Equal(t, initProtocol, protocolFallback.protocol)
	}

	// Retry fallback protocol
	for i := 0; i < int(maxRetries); i++ {
		protocolFallback.BackoffTimer() // simulate retry
		ok := selectNextProtocol(&log, protocolFallback, protocolSelector, false)
		assert.True(t, ok)
		fallback, ok := protocolSelector.Fallback()
		assert.True(t, ok)
		assert.Equal(t, fallback, protocolFallback.protocol)
	}

	currentGlobalProtocol := protocolSelector.Current()
	assert.Equal(t, initProtocol, currentGlobalProtocol)

	// No protocol to fallback, return error
	protocolFallback.BackoffTimer() // simulate retry
	ok := selectNextProtocol(&log, protocolFallback, protocolSelector, false)
	assert.False(t, ok)

	protocolFallback.reset()
	protocolFallback.BackoffTimer() // simulate retry
	ok = selectNextProtocol(&log, protocolFallback, protocolSelector, false)
	assert.True(t, ok)
	assert.Equal(t, initProtocol, protocolFallback.protocol)

	protocolFallback.reset()
	protocolFallback.BackoffTimer() // simulate retry
	ok = selectNextProtocol(&log, protocolFallback, protocolSelector, true)
	// Check that we get a true after the first try itself when this flag is true. This allows us to immediately
	// switch protocols.
	assert.True(t, ok)
}
