package origin

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

func TestRefreshAuthBackoff(t *testing.T) {
	logger := logrus.New()
	logger.Level = logrus.ErrorLevel

	var wait time.Duration
	timeAfter = func(d time.Duration) <-chan time.Time {
		wait = d
		return time.After(d)
	}

	s := NewSupervisor(&TunnelConfig{Logger: logger}, uuid.New())
	backoff := &BackoffHandler{MaxRetries: 3}
	auth := func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return nil, fmt.Errorf("authentication failure")
	}

	// authentication failures should consume the backoff
	for i := uint(0); i < backoff.MaxRetries; i++ {
		retryChan, err := s.refreshAuth(context.Background(), backoff, auth)
		assert.NoError(t, err)
		assert.NotNil(t, retryChan)
		assert.Equal(t, (1<<i)*time.Second, wait)
	}
	retryChan, err := s.refreshAuth(context.Background(), backoff, auth)
	assert.Error(t, err)
	assert.Nil(t, retryChan)

	// now we actually make contact with the remote server
	_, _ = s.refreshAuth(context.Background(), backoff, func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return tunnelpogs.NewAuthUnknown(errors.New("auth unknown"), 19), nil
	})

	// The backoff timer should have been reset. To confirm this, make timeNow
	// return a value after the backoff timer's grace period
	timeNow = func() time.Time {
		expectedGracePeriod := time.Duration(time.Second * 2 << backoff.MaxRetries)
		return time.Now().Add(expectedGracePeriod * 2)
	}
	_, ok := backoff.GetBackoffDuration(context.Background())
	assert.True(t, ok)
}

func TestRefreshAuthSuccess(t *testing.T) {
	logger := logrus.New()
	logger.Level = logrus.ErrorLevel

	var wait time.Duration
	timeAfter = func(d time.Duration) <-chan time.Time {
		wait = d
		return time.After(d)
	}

	s := NewSupervisor(&TunnelConfig{Logger: logger}, uuid.New())
	backoff := &BackoffHandler{MaxRetries: 3}
	auth := func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return tunnelpogs.NewAuthSuccess([]byte("jwt"), 19), nil
	}

	retryChan, err := s.refreshAuth(context.Background(), backoff, auth)
	assert.NoError(t, err)
	assert.NotNil(t, retryChan)
	assert.Equal(t, 19*time.Hour, wait)

	token, err := s.ReconnectToken()
	assert.NoError(t, err)
	assert.Equal(t, []byte("jwt"), token)
}

func TestRefreshAuthUnknown(t *testing.T) {
	logger := logrus.New()
	logger.Level = logrus.ErrorLevel

	var wait time.Duration
	timeAfter = func(d time.Duration) <-chan time.Time {
		wait = d
		return time.After(d)
	}

	s := NewSupervisor(&TunnelConfig{Logger: logger}, uuid.New())
	backoff := &BackoffHandler{MaxRetries: 3}
	auth := func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return tunnelpogs.NewAuthUnknown(errors.New("auth unknown"), 19), nil
	}

	retryChan, err := s.refreshAuth(context.Background(), backoff, auth)
	assert.NoError(t, err)
	assert.NotNil(t, retryChan)
	assert.Equal(t, 19*time.Hour, wait)

	token, err := s.ReconnectToken()
	assert.Equal(t, errJWTUnset, err)
	assert.Nil(t, token)
}

func TestRefreshAuthFail(t *testing.T) {
	logger := logrus.New()
	logger.Level = logrus.ErrorLevel

	s := NewSupervisor(&TunnelConfig{Logger: logger}, uuid.New())
	backoff := &BackoffHandler{MaxRetries: 3}
	auth := func(ctx context.Context, n int) (tunnelpogs.AuthOutcome, error) {
		return tunnelpogs.NewAuthFail(errors.New("auth fail")), nil
	}

	retryChan, err := s.refreshAuth(context.Background(), backoff, auth)
	assert.Error(t, err)
	assert.Nil(t, retryChan)

	token, err := s.ReconnectToken()
	assert.Equal(t, errJWTUnset, err)
	assert.Nil(t, token)
}
