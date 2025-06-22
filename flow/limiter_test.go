package flow_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/flow"
)

func TestFlowLimiter_Unlimited(t *testing.T) {
	unlimitedLimiter := flow.NewLimiter(0)

	for i := 0; i < 1000; i++ {
		err := unlimitedLimiter.Acquire("test")
		require.NoError(t, err)
	}
}

func TestFlowLimiter_Limited(t *testing.T) {
	maxFlows := uint64(5)
	limiter := flow.NewLimiter(maxFlows)

	for i := uint64(0); i < maxFlows; i++ {
		err := limiter.Acquire("test")
		require.NoError(t, err)
	}

	err := limiter.Acquire("should fail")
	require.ErrorIs(t, err, flow.ErrTooManyActiveFlows)
}

func TestFlowLimiter_AcquireAndReleaseFlow(t *testing.T) {
	maxFlows := uint64(5)
	limiter := flow.NewLimiter(maxFlows)

	// Acquire the maximum number of flows
	for i := uint64(0); i < maxFlows; i++ {
		err := limiter.Acquire("test")
		require.NoError(t, err)
	}

	// Validate acquire 1 more flows fails
	err := limiter.Acquire("should fail")
	require.ErrorIs(t, err, flow.ErrTooManyActiveFlows)

	// Release the maximum number of flows
	for i := uint64(0); i < maxFlows; i++ {
		limiter.Release()
	}

	// Validate acquire 1 more flows works
	err = limiter.Acquire("shouldn't fail")
	require.NoError(t, err)

	// Release a 10x the number of max flows
	for i := uint64(0); i < 10*maxFlows; i++ {
		limiter.Release()
	}

	// Validate it still can only acquire a value = number max flows.
	for i := uint64(0); i < maxFlows; i++ {
		err := limiter.Acquire("test")
		require.NoError(t, err)
	}
	err = limiter.Acquire("should fail")
	require.ErrorIs(t, err, flow.ErrTooManyActiveFlows)
}

func TestFlowLimiter_SetLimit(t *testing.T) {
	maxFlows := uint64(5)
	limiter := flow.NewLimiter(maxFlows)

	// Acquire the maximum number of flows
	for i := uint64(0); i < maxFlows; i++ {
		err := limiter.Acquire("test")
		require.NoError(t, err)
	}

	// Validate acquire 1 more flows fails
	err := limiter.Acquire("should fail")
	require.ErrorIs(t, err, flow.ErrTooManyActiveFlows)

	// Set the flow limiter to support one more request
	limiter.SetLimit(maxFlows + 1)

	// Validate acquire 1 more flows now works
	err = limiter.Acquire("shouldn't fail")
	require.NoError(t, err)

	// Validate acquire 1 more flows doesn't work because we already reached the limit
	err = limiter.Acquire("should fail")
	require.ErrorIs(t, err, flow.ErrTooManyActiveFlows)

	// Release all flows
	for i := uint64(0); i < maxFlows+1; i++ {
		limiter.Release()
	}

	// Validate 1 flow works again
	err = limiter.Acquire("shouldn't fail")
	require.NoError(t, err)

	// Set the flow limit to 1
	limiter.SetLimit(1)

	// Validate acquire 1 more flows doesn't work
	err = limiter.Acquire("should fail")
	require.ErrorIs(t, err, flow.ErrTooManyActiveFlows)

	// Set the flow limit to unlimited
	limiter.SetLimit(0)

	// Validate it can acquire a lot of flows because it is now unlimited.
	for i := uint64(0); i < 10*maxFlows; i++ {
		err := limiter.Acquire("shouldn't fail")
		require.NoError(t, err)
	}
}
