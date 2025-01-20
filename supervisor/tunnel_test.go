package supervisor

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go"
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

func (dmf *dynamicMockFetcher) fetch() edgediscovery.PercentageFetcher {
	return func() (edgediscovery.ProtocolPercents, error) {
		return dmf.protocolPercents, dmf.err
	}
}

func immediateTimeAfter(time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	c <- time.Now()
	return c
}

func TestWaitForBackoffFallback(t *testing.T) {
	maxRetries := uint(3)
	backoff := retry.NewBackoff(maxRetries, 40*time.Millisecond, false)
	backoff.Clock.After = immediateTimeAfter
	log := zerolog.Nop()
	resolveTTL := 10 * time.Second
	mockFetcher := dynamicMockFetcher{
		protocolPercents: edgediscovery.ProtocolPercents{edgediscovery.ProtocolPercent{Protocol: "quic", Percentage: 100}},
	}
	protocolSelector, err := connection.NewProtocolSelector(
		"auto",
		"",
		false,
		false,
		mockFetcher.fetch(),
		resolveTTL,
		&log,
	)
	assert.NoError(t, err)

	initProtocol := protocolSelector.Current()
	assert.Equal(t, connection.QUIC, initProtocol)

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
	protoFallback.BackoffTimer() // simulate retry
	ok := selectNextProtocol(&log, protoFallback, protocolSelector, nil)
	assert.True(t, ok)
	fallback, ok := protocolSelector.Fallback()
	assert.True(t, ok)
	assert.Equal(t, fallback, protoFallback.protocol)
	assert.Equal(t, connection.HTTP2, protoFallback.protocol)

	currentGlobalProtocol := protocolSelector.Current()
	assert.Equal(t, initProtocol, currentGlobalProtocol)

	// Simulate max retries again (retries reset after protocol switch)
	for i := 0; i < int(maxRetries); i++ {
		protoFallback.BackoffTimer()
	}
	// No protocol to fallback, return error
	ok = selectNextProtocol(&log, protoFallback, protocolSelector, nil)
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
		"",
		false,
		false,
		mockFetcher.fetch(),
		resolveTTL,
		&log,
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
