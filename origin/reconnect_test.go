package origin

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/retry"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

func TestRefreshAuthBackoff(t *testing.T) {
	rcm := newReconnectCredentialManager(t.Name(), t.Name(), 4)

	var wait time.Duration
	retry.Clock.After = func(d time.Duration) <-chan time.Time {
		wait = d
		return time.After(d)
	}
	backoff := &retry.BackoffHandler{MaxRetries: 3}
	auth := func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return nil, fmt.Errorf("authentication failure")
	}

	// authentication failures should consume the backoff
	for i := uint(0); i < backoff.MaxRetries; i++ {
		retryChan, err := rcm.RefreshAuth(context.Background(), backoff, auth)
		require.NoError(t, err)
		require.NotNil(t, retryChan)
		require.Greater(t, wait.Seconds(), 0.0)
		require.Less(t, wait.Seconds(), float64((1<<(i+1))*time.Second))
	}
	retryChan, err := rcm.RefreshAuth(context.Background(), backoff, auth)
	require.Error(t, err)
	require.Nil(t, retryChan)

	// now we actually make contact with the remote server
	_, _ = rcm.RefreshAuth(context.Background(), backoff, func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return tunnelpogs.NewAuthUnknown(errors.New("auth unknown"), 19), nil
	})

	// The backoff timer should have been reset. To confirm this, make timeNow
	// return a value after the backoff timer's grace period
	retry.Clock.Now = func() time.Time {
		expectedGracePeriod := time.Duration(time.Second * 2 << backoff.MaxRetries)
		return time.Now().Add(expectedGracePeriod * 2)
	}
	_, ok := backoff.GetMaxBackoffDuration(context.Background())
	require.True(t, ok)
}

func TestRefreshAuthSuccess(t *testing.T) {
	rcm := newReconnectCredentialManager(t.Name(), t.Name(), 4)

	var wait time.Duration
	retry.Clock.After = func(d time.Duration) <-chan time.Time {
		wait = d
		return time.After(d)
	}

	backoff := &retry.BackoffHandler{MaxRetries: 3}
	auth := func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return tunnelpogs.NewAuthSuccess([]byte("jwt"), 19), nil
	}

	retryChan, err := rcm.RefreshAuth(context.Background(), backoff, auth)
	assert.NoError(t, err)
	assert.NotNil(t, retryChan)
	assert.Equal(t, 19*time.Hour, wait)

	token, err := rcm.ReconnectToken()
	assert.NoError(t, err)
	assert.Equal(t, []byte("jwt"), token)
}

func TestRefreshAuthUnknown(t *testing.T) {
	rcm := newReconnectCredentialManager(t.Name(), t.Name(), 4)

	var wait time.Duration
	retry.Clock.After = func(d time.Duration) <-chan time.Time {
		wait = d
		return time.After(d)
	}

	backoff := &retry.BackoffHandler{MaxRetries: 3}
	auth := func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return tunnelpogs.NewAuthUnknown(errors.New("auth unknown"), 19), nil
	}

	retryChan, err := rcm.RefreshAuth(context.Background(), backoff, auth)
	assert.NoError(t, err)
	assert.NotNil(t, retryChan)
	assert.Equal(t, 19*time.Hour, wait)

	token, err := rcm.ReconnectToken()
	assert.Equal(t, errJWTUnset, err)
	assert.Nil(t, token)
}

func TestRefreshAuthFail(t *testing.T) {
	rcm := newReconnectCredentialManager(t.Name(), t.Name(), 4)

	backoff := &retry.BackoffHandler{MaxRetries: 3}
	auth := func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return tunnelpogs.NewAuthFail(errors.New("auth fail")), nil
	}

	retryChan, err := rcm.RefreshAuth(context.Background(), backoff, auth)
	assert.Error(t, err)
	assert.Nil(t, retryChan)

	token, err := rcm.ReconnectToken()
	assert.Equal(t, errJWTUnset, err)
	assert.Nil(t, token)
}
