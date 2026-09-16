package quicktunnelauth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQuickTunnelAuthAssertionValidator(t *testing.T) {
	t.Parallel()

	validator, err := NewQuickTunnelAuthAssertionValidator()
	require.NoError(t, err)
	t.Cleanup(validator.Close)
	require.NotNil(t, validator)
	assert.Equal(t, quickTunnelAuthBrokerJWKSURL, validator.jwksURL.String())
	require.NotNil(t, validator.httpClient)
	assert.Equal(t, quickTunnelAuthBrokerJWKSTimeout, validator.httpClient.Timeout)
	require.NotNil(t, validator.httpClient.CheckRedirect)
	require.ErrorIs(t, validator.httpClient.CheckRedirect(nil, nil), http.ErrUseLastResponse)
	require.NotNil(t, validator.now)
	assert.Empty(t, validator.jwks.keySet.Keys)
}

func TestQuickTunnelAuthAssertionValidatorCloseStopsRefreshWorker(t *testing.T) {
	t.Parallel()

	validator, err := NewQuickTunnelAuthAssertionValidator()
	require.NoError(t, err)
	validator.Close()
	validator.Close()

	key, err := validator.verificationKey(context.Background(), "test-key")
	require.ErrorIs(t, err, errQuickTunnelAuthBrokerJWKSRefreshWorkerStopped)
	assert.Nil(t, key)
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyCachesAndRefreshes(t *testing.T) {
	t.Parallel()

	firstKey := newTestQuickTunnelAuthBrokerVerificationKey(t, "first-key")
	secondKey := newTestQuickTunnelAuthBrokerVerificationKey(t, "second-key")
	responses := [][]byte{
		marshalTestQuickTunnelAuthBrokerJWKS(t, firstKey),
		marshalTestQuickTunnelAuthBrokerJWKS(t, firstKey, secondKey),
	}
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(requestCount.Add(1)) - 1
		if index >= len(responses) {
			index = len(responses) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(responses[index]); err != nil {
			t.Errorf("write broker JWKS response: %v", err)
		}
	}))
	defer server.Close()

	validator := newTestQuickTunnelAuthAssertionValidator(t, server)
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	validator.now = func() time.Time { return now }
	key, err := validator.verificationKey(context.Background(), firstKey.KeyID)
	require.NoError(t, err)
	assert.Equal(t, firstKey.KeyID, key.KeyID)
	assert.Equal(t, int32(1), requestCount.Load())

	key, err = validator.verificationKey(context.Background(), firstKey.KeyID)
	require.NoError(t, err)
	assert.Equal(t, firstKey.KeyID, key.KeyID)
	assert.Equal(t, int32(1), requestCount.Load())

	now = now.Add(quickTunnelAuthBrokerJWKSRefreshCooldown)
	key, err = validator.verificationKey(context.Background(), secondKey.KeyID)
	require.NoError(t, err)
	assert.Equal(t, secondKey.KeyID, key.KeyID)
	assert.Equal(t, int32(2), requestCount.Load())
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyRateLimitsUnknownKeyRefresh(t *testing.T) {
	t.Parallel()

	knownKey := newTestQuickTunnelAuthBrokerVerificationKey(t, "known-key")
	response := marshalTestQuickTunnelAuthBrokerJWKS(t, knownKey)
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(response); err != nil {
			t.Errorf("write broker JWKS response: %v", err)
		}
	}))
	defer server.Close()

	validator := newTestQuickTunnelAuthAssertionValidator(t, server)
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	validator.now = func() time.Time { return now }
	_, err := validator.verificationKey(context.Background(), knownKey.KeyID)
	require.NoError(t, err)

	key, err := validator.verificationKey(context.Background(), "unknown-key")
	require.EqualError(t, err, "broker JWKS refresh is rate limited")
	assert.Nil(t, key)
	assert.Equal(t, int32(1), requestCount.Load())

	now = now.Add(quickTunnelAuthBrokerJWKSRefreshCooldown)
	key, err = validator.verificationKey(context.Background(), "unknown-key")
	require.EqualError(t, err, "broker assertion verification key is unavailable after JWKS refresh")
	assert.Nil(t, key)
	assert.Equal(t, int32(2), requestCount.Load())

	key, err = validator.verificationKey(context.Background(), "another-unknown-key")
	require.EqualError(t, err, "broker JWKS refresh is rate limited")
	assert.Nil(t, key)
	assert.Equal(t, int32(2), requestCount.Load())
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyRateLimitsFailedRefresh(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	validator := newTestQuickTunnelAuthAssertionValidator(t, server)
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	validator.now = func() time.Time { return now }

	key, err := validator.verificationKey(context.Background(), "unknown-key")
	require.ErrorContains(t, err, "broker JWKS endpoint returned status 503")
	assert.Nil(t, key)
	assert.Equal(t, int32(quickTunnelAuthBrokerJWKSMaxFetchAttempts), requestCount.Load())

	key, err = validator.verificationKey(context.Background(), "another-unknown-key")
	require.EqualError(t, err, "broker JWKS refresh is rate limited")
	assert.Nil(t, key)
	assert.Equal(t, int32(quickTunnelAuthBrokerJWKSMaxFetchAttempts), requestCount.Load())
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyRetriesTransientRefreshFailures(t *testing.T) {
	t.Parallel()

	verificationKey := newTestQuickTunnelAuthBrokerVerificationKey(t, "test-key")
	response := marshalTestQuickTunnelAuthBrokerJWKS(t, verificationKey)
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requestCount.Add(1) < quickTunnelAuthBrokerJWKSMaxFetchAttempts {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(response); err != nil {
			t.Errorf("write broker JWKS response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	validator := newTestQuickTunnelAuthAssertionValidator(t, server)
	key, err := validator.verificationKey(context.Background(), verificationKey.KeyID)
	require.NoError(t, err)
	require.NotNil(t, key)
	assert.Equal(t, verificationKey.KeyID, key.KeyID)
	assert.Equal(t, int32(quickTunnelAuthBrokerJWKSMaxFetchAttempts), requestCount.Load())
}

func TestQuickTunnelAuthAssertionValidatorRefreshJWKSBoundsRetryDuration(t *testing.T) {
	t.Parallel()

	validator, err := NewQuickTunnelAuthAssertionValidator()
	require.NoError(t, err)
	t.Cleanup(validator.Close)
	var requestCount atomic.Int32
	validator.httpClient.Transport = testQuickTunnelAuthRoundTripper(func(request *http.Request) (*http.Response, error) {
		requestCount.Add(1)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	err = validator.refreshJWKS(ctx, "unknown-key", new(time.Time))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), time.Second)
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyDoesNotRetryPermanentRefreshFailures(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	validator := newTestQuickTunnelAuthAssertionValidator(t, server)
	key, err := validator.verificationKey(context.Background(), "unknown-key")
	require.ErrorContains(t, err, "broker JWKS endpoint returned status 400")
	assert.Nil(t, key)
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyRejectsInvalidKeyID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		keyID         string
		expectedError string
	}{
		{name: "empty", expectedError: "broker assertion key ID cannot be empty"},
		{name: "leading whitespace", keyID: " test-key", expectedError: "broker assertion key ID cannot contain leading or trailing whitespace"},
		{name: "trailing whitespace", keyID: "test-key ", expectedError: "broker assertion key ID cannot contain leading or trailing whitespace"},
		{name: "oversized", keyID: strings.Repeat("k", quickTunnelAuthBrokerMaxKeyIDLength+1), expectedError: "broker assertion key ID exceeds 128 bytes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			verificationKey := newTestQuickTunnelAuthBrokerVerificationKey(t, "test-key")
			validator, requestCount := newConcurrentFetchTestValidator(t, verificationKey)
			key, err := validator.verificationKey(context.Background(), test.keyID)
			require.EqualError(t, err, test.expectedError)
			assert.Nil(t, key)
			assert.Zero(t, requestCount.Load())
		})
	}
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyAcceptsMaxLengthKeyID(t *testing.T) {
	t.Parallel()

	keyID := strings.Repeat("k", quickTunnelAuthBrokerMaxKeyIDLength)
	verificationKey := newTestQuickTunnelAuthBrokerVerificationKey(t, keyID)
	validator, requestCount := newConcurrentFetchTestValidator(t, verificationKey)

	key, err := validator.verificationKey(context.Background(), keyID)
	require.NoError(t, err)
	require.NotNil(t, key)
	assert.Equal(t, keyID, key.KeyID)
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyDeduplicatesConcurrentFetch(t *testing.T) {
	t.Parallel()

	verificationKey := newTestQuickTunnelAuthBrokerVerificationKey(t, "test-key")
	validator, requestCount := newConcurrentFetchTestValidator(t, verificationKey)
	runConcurrentVerificationKeyRequests(t, validator, verificationKey.KeyID)
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyDeduplicatesConcurrentRefresh(t *testing.T) {
	t.Parallel()

	firstKey := newTestQuickTunnelAuthBrokerVerificationKey(t, "first-key")
	secondKey := newTestQuickTunnelAuthBrokerVerificationKey(t, "second-key")
	validator, requestCount := newConcurrentFetchTestValidator(t, firstKey, secondKey)
	validator.jwks.keySet = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{firstKey}}
	runConcurrentVerificationKeyRequests(t, validator, secondKey.KeyID)
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestQuickTunnelAuthAssertionValidatorVerificationKeyReadsCacheDuringRefresh(t *testing.T) {
	t.Parallel()

	knownKey := newTestQuickTunnelAuthBrokerVerificationKey(t, "known-key")
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	var releaseRefreshOnce sync.Once
	release := func() {
		releaseRefreshOnce.Do(func() { close(releaseRefresh) })
	}
	t.Cleanup(release)

	response := marshalTestQuickTunnelAuthBrokerJWKS(t, knownKey)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(refreshStarted)
		<-releaseRefresh
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(response); err != nil {
			t.Errorf("write broker JWKS response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	validator := newTestQuickTunnelAuthAssertionValidator(t, server)
	validator.jwks.keySet = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{knownKey}}
	refreshResult := make(chan error, 1)
	go func() {
		_, err := validator.verificationKey(context.Background(), "unknown-key")
		refreshResult <- err
	}()

	select {
	case <-refreshStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for JWKS refresh to start")
	}

	key, err := validator.verificationKey(context.Background(), knownKey.KeyID)
	require.NoError(t, err)
	require.NotNil(t, key)
	assert.Equal(t, knownKey.KeyID, key.KeyID)

	release()
	select {
	case err := <-refreshResult:
		require.EqualError(t, err, "broker assertion verification key is unavailable after JWKS refresh")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for JWKS refresh to finish")
	}
}

func TestQuickTunnelAuthAssertionValidatorFetchJWKSReportsTransportErrors(t *testing.T) {
	t.Parallel()

	t.Run("request failure", func(t *testing.T) {
		t.Parallel()

		validator, err := NewQuickTunnelAuthAssertionValidator()
		require.NoError(t, err)
		t.Cleanup(validator.Close)
		validator.httpClient.Transport = testQuickTunnelAuthRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, assert.AnError
		})

		keySet, err := validator.fetchJWKS(context.Background())
		require.ErrorContains(t, err, "request broker JWKS")
		assert.Nil(t, keySet)
	})

	t.Run("response read failure", func(t *testing.T) {
		t.Parallel()

		validator, err := NewQuickTunnelAuthAssertionValidator()
		require.NoError(t, err)
		t.Cleanup(validator.Close)
		validator.httpClient.Transport = testQuickTunnelAuthRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(iotest.ErrReader(assert.AnError)),
				Header:     make(http.Header),
			}, nil
		})

		keySet, err := validator.fetchJWKS(context.Background())
		require.ErrorContains(t, err, "read broker JWKS response")
		assert.Nil(t, keySet)
	})
}

func TestQuickTunnelAuthAssertionValidatorFetchJWKSRejectsInvalidResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		body          []byte
		expectedError string
	}{
		{name: "non-OK status", status: http.StatusInternalServerError, expectedError: "broker JWKS endpoint returned status 500"},
		{name: "malformed JSON", status: http.StatusOK, body: []byte("{"), expectedError: "parse broker JWKS response"},
		{name: "empty key set", status: http.StatusOK, body: []byte(`{"keys":[]}`), expectedError: "broker JWKS contains no verification keys"},
		{name: "oversized response", status: http.StatusOK, body: bytes.Repeat([]byte("x"), quickTunnelAuthBrokerMaxJWKSResponseSize+1), expectedError: "broker JWKS response exceeds"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				if _, err := w.Write(test.body); err != nil {
					t.Errorf("write broker JWKS response: %v", err)
				}
			}))
			defer server.Close()

			validator := newTestQuickTunnelAuthAssertionValidator(t, server)
			keySet, err := validator.fetchJWKS(context.Background())
			require.ErrorContains(t, err, test.expectedError)
			assert.Nil(t, keySet)
		})
	}
}

func TestQuickTunnelAuthAssertionValidatorFetchJWKSRejectsRedirect(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/keys", http.StatusFound)
	}))
	defer server.Close()

	validator := newTestQuickTunnelAuthAssertionValidator(t, server)
	keySet, err := validator.fetchJWKS(context.Background())
	require.EqualError(t, err, "broker JWKS endpoint returned status 302")
	assert.Nil(t, keySet)
}

func TestQuickTunnelAuthAssertionValidatorFetchJWKSRespectsContext(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	validator := newTestQuickTunnelAuthAssertionValidator(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	keySet, err := validator.fetchJWKS(ctx)
	require.ErrorContains(t, err, "context canceled")
	assert.Nil(t, keySet)
}

func TestValidateQuickTunnelAuthBrokerJWKS(t *testing.T) {
	t.Parallel()

	privateKey, validKey := newTestQuickTunnelAuthBrokerKeyPair(t, "valid-key")
	missingKeyID := validKey
	missingKeyID.KeyID = ""
	whitespaceKeyID := validKey
	whitespaceKeyID.KeyID = " valid-key"
	oversizedKeyID := validKey
	oversizedKeyID.KeyID = strings.Repeat("k", quickTunnelAuthBrokerMaxKeyIDLength+1)
	wrongAlgorithm := validKey
	wrongAlgorithm.Algorithm = string(jose.RS256)
	wrongUse := validKey
	wrongUse.Use = "enc"
	privateJWK := validKey
	privateJWK.Key = privateKey
	p384PrivateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	wrongCurve := validKey
	wrongCurve.Key = &p384PrivateKey.PublicKey
	invalidKey := validKey
	invalidKey.Key = nil

	tests := []struct {
		name          string
		keySet        *jose.JSONWebKeySet
		expectedError string
	}{
		{name: "nil key set", expectedError: "contains no verification keys"},
		{name: "empty key set", keySet: &jose.JSONWebKeySet{}, expectedError: "contains no verification keys"},
		{name: "duplicate key IDs", keySet: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{validKey, validKey}}, expectedError: "duplicate key IDs"},
		{name: "missing key ID", keySet: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{missingKeyID}}, expectedError: "invalid key ID"},
		{name: "key ID with whitespace", keySet: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{whitespaceKeyID}}, expectedError: "invalid key ID"},
		{name: "oversized key ID", keySet: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{oversizedKeyID}}, expectedError: "invalid key ID"},
		{name: "wrong algorithm", keySet: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{wrongAlgorithm}}, expectedError: "unsupported metadata"},
		{name: "wrong use", keySet: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{wrongUse}}, expectedError: "unsupported metadata"},
		{name: "private key", keySet: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{privateJWK}}, expectedError: "invalid verification key"},
		{name: "wrong curve", keySet: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{wrongCurve}}, expectedError: "not an ES256 public key"},
		{name: "invalid key", keySet: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{invalidKey}}, expectedError: "invalid verification key"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, validateQuickTunnelAuthBrokerJWKS(test.keySet), test.expectedError)
		})
	}

	require.NoError(t, validateQuickTunnelAuthBrokerJWKS(&jose.JSONWebKeySet{Keys: []jose.JSONWebKey{validKey}}))
}

func newConcurrentFetchTestValidator(
	t *testing.T,
	keys ...jose.JSONWebKey,
) (*QuickTunnelAuthAssertionValidator, *atomic.Int32) {
	t.Helper()

	response := marshalTestQuickTunnelAuthBrokerJWKS(t, keys...)
	requestCount := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(response); err != nil {
			t.Errorf("write broker JWKS response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return newTestQuickTunnelAuthAssertionValidator(t, server), requestCount
}

func runConcurrentVerificationKeyRequests(
	t *testing.T,
	validator *QuickTunnelAuthAssertionValidator,
	keyID string,
) {
	t.Helper()

	const concurrentRequests = 8

	start := make(chan struct{})
	errors := make(chan error, concurrentRequests)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrentRequests)
	for range concurrentRequests {
		go func() {
			defer waitGroup.Done()
			<-start
			_, err := validator.verificationKey(context.Background(), keyID)
			errors <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(errors)

	for err := range errors {
		require.NoError(t, err)
	}
}

func newTestQuickTunnelAuthAssertionValidator(t *testing.T, server *httptest.Server) *QuickTunnelAuthAssertionValidator {
	t.Helper()

	validator, err := NewQuickTunnelAuthAssertionValidator()
	require.NoError(t, err)
	t.Cleanup(validator.Close)
	jwksURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	validator.jwksURL = *jwksURL
	validator.httpClient.Transport = server.Client().Transport
	return validator
}

func newTestQuickTunnelAuthBrokerKeyPair(t *testing.T, keyID string) (*ecdsa.PrivateKey, jose.JSONWebKey) {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return privateKey, jose.JSONWebKey{
		Key:       &privateKey.PublicKey,
		KeyID:     keyID,
		Algorithm: string(jose.ES256),
		Use:       quickTunnelAuthBrokerKeyUse,
	}
}

func newTestQuickTunnelAuthBrokerVerificationKey(t *testing.T, keyID string) jose.JSONWebKey {
	t.Helper()

	_, verificationKey := newTestQuickTunnelAuthBrokerKeyPair(t, keyID)
	return verificationKey
}

type testQuickTunnelAuthRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip testQuickTunnelAuthRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func marshalTestQuickTunnelAuthBrokerJWKS(t *testing.T, keys ...jose.JSONWebKey) []byte {
	t.Helper()

	payload, err := json.Marshal(jose.JSONWebKeySet{Keys: keys})
	require.NoError(t, err)
	return payload
}
