package session_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/session"
)

func TestSessionLimiter_Unlimited(t *testing.T) {
	unlimitedLimiter := session.NewLimiter(0)

	for i := 0; i < 1000; i++ {
		err := unlimitedLimiter.Acquire("test")
		require.NoError(t, err)
	}
}

func TestSessionLimiter_Limited(t *testing.T) {
	maxSessions := uint64(5)
	limiter := session.NewLimiter(maxSessions)

	for i := uint64(0); i < maxSessions; i++ {
		err := limiter.Acquire("test")
		require.NoError(t, err)
	}

	err := limiter.Acquire("should fail")
	require.ErrorIs(t, err, session.ErrTooManyActiveSessions)
}

func TestSessionLimiter_AcquireAndReleaseSession(t *testing.T) {
	maxSessions := uint64(5)
	limiter := session.NewLimiter(maxSessions)

	// Acquire the maximum number of sessions
	for i := uint64(0); i < maxSessions; i++ {
		err := limiter.Acquire("test")
		require.NoError(t, err)
	}

	// Validate acquire 1 more sessions fails
	err := limiter.Acquire("should fail")
	require.ErrorIs(t, err, session.ErrTooManyActiveSessions)

	// Release the maximum number of sessions
	for i := uint64(0); i < maxSessions; i++ {
		limiter.Release()
	}

	// Validate acquire 1 more sessions works
	err = limiter.Acquire("shouldn't fail")
	require.NoError(t, err)

	// Release a 10x the number of max sessions
	for i := uint64(0); i < 10*maxSessions; i++ {
		limiter.Release()
	}

	// Validate it still can only acquire a value = number max sessions.
	for i := uint64(0); i < maxSessions; i++ {
		err := limiter.Acquire("test")
		require.NoError(t, err)
	}
	err = limiter.Acquire("should fail")
	require.ErrorIs(t, err, session.ErrTooManyActiveSessions)
}

func TestSessionLimiter_SetLimit(t *testing.T) {
	maxSessions := uint64(5)
	limiter := session.NewLimiter(maxSessions)

	// Acquire the maximum number of sessions
	for i := uint64(0); i < maxSessions; i++ {
		err := limiter.Acquire("test")
		require.NoError(t, err)
	}

	// Validate acquire 1 more sessions fails
	err := limiter.Acquire("should fail")
	require.ErrorIs(t, err, session.ErrTooManyActiveSessions)

	// Set the session limiter to support one more request
	limiter.SetLimit(maxSessions + 1)

	// Validate acquire 1 more sessions now works
	err = limiter.Acquire("shouldn't fail")
	require.NoError(t, err)

	// Validate acquire 1 more sessions doesn't work because we already reached the limit
	err = limiter.Acquire("should fail")
	require.ErrorIs(t, err, session.ErrTooManyActiveSessions)

	// Release all sessions
	for i := uint64(0); i < maxSessions+1; i++ {
		limiter.Release()
	}

	// Validate 1 session works again
	err = limiter.Acquire("shouldn't fail")
	require.NoError(t, err)

	// Set the session limit to 1
	limiter.SetLimit(1)

	// Validate acquire 1 more sessions doesn't work
	err = limiter.Acquire("should fail")
	require.ErrorIs(t, err, session.ErrTooManyActiveSessions)

	// Set the session limit to unlimited
	limiter.SetLimit(0)

	// Validate it can acquire a lot of sessions because it is now unlimited.
	for i := uint64(0); i < 10*maxSessions; i++ {
		err := limiter.Acquire("shouldn't fail")
		require.NoError(t, err)
	}
}
