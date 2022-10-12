package supervisor

import (
	"testing"
	"time"

	"github.com/lucas-clemente/quic-go"
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
	namedTunnel := &connection.NamedTunnelProperties{}
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
		false,
	)
	assert.NoError(t, err)

	initProtocol := protocolSelector.Current()
	assert.Equal(t, connection.HTTP2, initProtocol)

	protoFallback := &protocolFallback{
		backoff,
		initProtocol,
		false,
	}

	// Retry #0 and #1. At retry #2, we switch protocol, so the fallback loop has one more retry than this
	for i := 0; i < int(maxRetries-1); i++ {
		protoFallback.BackoffTimer() // simulate retry
		ok := selectNextProtocol(&log, protoFallback, protocolSelector, nil)
		assert.True(t, ok)
		assert.Equal(t, initProtocol, protoFallback.protocol)
	}

	// Retry fallback protocol
	for i := 0; i < int(maxRetries); i++ {
		protoFallback.BackoffTimer() // simulate retry
		ok := selectNextProtocol(&log, protoFallback, protocolSelector, nil)
		assert.True(t, ok)
		fallback, ok := protocolSelector.Fallback()
		assert.True(t, ok)
		assert.Equal(t, fallback, protoFallback.protocol)
	}

	currentGlobalProtocol := protocolSelector.Current()
	assert.Equal(t, initProtocol, currentGlobalProtocol)

	// No protocol to fallback, return error
	protoFallback.BackoffTimer() // simulate retry
	ok := selectNextProtocol(&log, protoFallback, protocolSelector, nil)
	assert.False(t, ok)

	protoFallback.reset()
	protoFallback.BackoffTimer() // simulate retry
	ok = selectNextProtocol(&log, protoFallback, protocolSelector, nil)
	assert.True(t, ok)
	assert.Equal(t, initProtocol, protoFallback.protocol)

	protoFallback.reset()
	protoFallback.BackoffTimer() // simulate retry
	ok = selectNextProtocol(&log, protoFallback, protocolSelector, &quic.IdleTimeoutError{})
	// Check that we get a true after the first try itself when this flag is true. This allows us to immediately
	// switch protocols when there is a fallback.
	assert.True(t, ok)

	// But if there is no fallback available, then we exhaust the retries despite the type of error.
	// The reason why there's no fallback available is because we pick a specific protocol instead of letting it be auto.
	protocolSelector, err = connection.NewProtocolSelector(
		"quic",
		warpRoutingEnabled,
		namedTunnel,
		mockFetcher.fetch(),
		resolveTTL,
		&log,
		false,
	)
	assert.NoError(t, err)
	protoFallback = &protocolFallback{backoff, protocolSelector.Current(), false}
	for i := 0; i < int(maxRetries-1); i++ {
		protoFallback.BackoffTimer() // simulate retry
		ok := selectNextProtocol(&log, protoFallback, protocolSelector, &quic.IdleTimeoutError{})
		assert.True(t, ok)
		assert.Equal(t, connection.QUIC, protoFallback.protocol)
	}
	// And finally it fails as it should, with no fallback.
	protoFallback.BackoffTimer()
	ok = selectNextProtocol(&log, protoFallback, protocolSelector, &quic.IdleTimeoutError{})
	assert.False(t, ok)
}
