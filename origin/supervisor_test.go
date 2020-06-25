package origin

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/logger"
	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

func testConfig(logger logger.Service) *TunnelConfig {
	metrics := TunnelMetrics{}

	metrics.authSuccess = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: tunnelSubsystem,
			Name:      "tunnel_authenticate_success",
			Help:      "Count of successful tunnel authenticate",
		},
	)

	metrics.authFail = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: tunnelSubsystem,
			Name:      "tunnel_authenticate_fail",
			Help:      "Count of tunnel authenticate errors by type",
		},
		[]string{"error"},
	)
	return &TunnelConfig{Logger: logger, Metrics: &metrics}
}

func TestRefreshAuthBackoff(t *testing.T) {
	logger := logger.NewOutputWriter(logger.NewMockWriteManager())

	var wait time.Duration
	timeAfter = func(d time.Duration) <-chan time.Time {
		wait = d
		return time.After(d)
	}

	s, err := NewSupervisor(testConfig(logger), uuid.New(), nil)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
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
	logger := logger.NewOutputWriter(logger.NewMockWriteManager())

	var wait time.Duration
	timeAfter = func(d time.Duration) <-chan time.Time {
		wait = d
		return time.After(d)
	}

	s, err := NewSupervisor(testConfig(logger), uuid.New(), nil)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
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
	logger := logger.NewOutputWriter(logger.NewMockWriteManager())

	var wait time.Duration
	timeAfter = func(d time.Duration) <-chan time.Time {
		wait = d
		return time.After(d)
	}

	s, err := NewSupervisor(testConfig(logger), uuid.New(), nil)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
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
	logger := logger.NewOutputWriter(logger.NewMockWriteManager())

	s, err := NewSupervisor(testConfig(logger), uuid.New(), nil)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
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
