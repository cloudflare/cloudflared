package quicktunnelauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/rs/zerolog/log"
)

const (
	quickTunnelAuthBrokerJWKSURL = "https://login.trycloudflare.com/.well-known/jwks.json"
	// Five seconds bounds the complete refresh sequence, including retries, so
	// one refresh cannot stall the serialized worker indefinitely.
	quickTunnelAuthBrokerJWKSTimeout = 5 * time.Second
	// Cached keys expire so broker key retirement and revocation take effect for
	// long-running cloudflared processes.
	quickTunnelAuthBrokerJWKSCacheTTL = 24 * time.Hour
	// One refresh sequence per minute bounds broker traffic caused by
	// attacker-controlled unknown key IDs.
	quickTunnelAuthBrokerJWKSRefreshCooldown = time.Minute
	// Three attempts allow one initial request and two transient-failure retries.
	quickTunnelAuthBrokerJWKSMaxFetchAttempts = 3
	// Backoffs of 100ms and 200ms add at most 300ms within the five-second
	// refresh budget while giving brief transient failures time to clear.
	quickTunnelAuthBrokerJWKSInitialRetryBackoff = 100 * time.Millisecond
	quickTunnelAuthBrokerKeyUse                  = "sig"
	// Common UUID and digest-based key IDs are at most 64 bytes; 128 bytes leaves
	// headroom while bounding attacker-controlled input.
	quickTunnelAuthBrokerMaxKeyIDLength = 128
	// Broker key sets should contain only a few P-256 keys; one MiB is a generous
	// ceiling that prevents an unbounded response read.
	quickTunnelAuthBrokerMaxJWKSResponseSize = 1 << 20
)

var errQuickTunnelAuthBrokerJWKSRefreshWorkerStopped = errors.New("broker JWKS refresh worker is stopped")

type retryableQuickTunnelAuthBrokerJWKSError struct {
	err error
}

func (e *retryableQuickTunnelAuthBrokerJWKSError) Error() string {
	return e.err.Error()
}

func (e *retryableQuickTunnelAuthBrokerJWKSError) Unwrap() error {
	return e.err
}

type quickTunnelAuthBrokerJWKSRefreshNotification struct {
	keyID  string
	result chan error
}

type quickTunnelAuthBrokerJWKSCache struct {
	mu        sync.RWMutex
	keySet    jose.JSONWebKeySet
	expiresAt time.Time
}

// QuickTunnelAuthAssertionValidator caches broker verification keys used to
// validate callback-bound broker assertions.
type QuickTunnelAuthAssertionValidator struct {
	jwksURL              url.URL
	httpClient           *http.Client
	now                  func() time.Time
	jwks                 quickTunnelAuthBrokerJWKSCache
	refreshNotifications chan quickTunnelAuthBrokerJWKSRefreshNotification
	cancelRefreshWorker  context.CancelFunc
	refreshWorkerStopped chan struct{}
	closeOnce            sync.Once
}

// NewQuickTunnelAuthAssertionValidator creates a broker assertion validator.
func NewQuickTunnelAuthAssertionValidator() (*QuickTunnelAuthAssertionValidator, error) {
	jwksURL, err := url.Parse(quickTunnelAuthBrokerJWKSURL)
	if err != nil {
		return nil, fmt.Errorf("parse Quick Tunnel authentication broker JWKS URL: %w", err)
	}

	workerCtx, cancelRefreshWorker := context.WithCancel(context.Background()) //nolint:gosec
	validator := &QuickTunnelAuthAssertionValidator{
		jwksURL: *jwksURL,
		httpClient: &http.Client{
			Timeout: quickTunnelAuthBrokerJWKSTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now:                  time.Now,
		refreshNotifications: make(chan quickTunnelAuthBrokerJWKSRefreshNotification),
		cancelRefreshWorker:  cancelRefreshWorker,
		refreshWorkerStopped: make(chan struct{}),
	}
	go validator.runJWKSRefreshWorker(workerCtx)
	return validator, nil
}

// Close stops the validator's JWKS refresh worker.
func (v *QuickTunnelAuthAssertionValidator) Close() {
	v.closeOnce.Do(func() {
		v.cancelRefreshWorker()
		<-v.refreshWorkerStopped
	})
}

// verificationKey returns the cached key for keyID, notifying the refresh
// worker when the key is not cached.
func (v *QuickTunnelAuthAssertionValidator) verificationKey(ctx context.Context, keyID string) (*jose.JSONWebKey, error) {
	if err := validateQuickTunnelAuthBrokerKeyID(keyID); err != nil {
		return nil, fmt.Errorf("broker assertion key ID %w", err)
	}

	key, err := v.cachedVerificationKey(keyID)
	if err == nil && key != nil {
		return key, nil
	}

	if err := v.notifyJWKSRefresh(ctx, keyID); err != nil {
		return nil, err
	}

	key, err = v.cachedVerificationKey(keyID)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, errors.New("broker assertion verification key is unavailable after JWKS refresh")
	}
	return key, nil
}

// cachedVerificationKey reads a verification key without blocking concurrent
// cache readers or holding the cache lock during a refresh request.
func (v *QuickTunnelAuthAssertionValidator) cachedVerificationKey(keyID string) (*jose.JSONWebKey, error) {
	v.jwks.mu.RLock()
	defer v.jwks.mu.RUnlock()
	if !v.now().Before(v.jwks.expiresAt) {
		if !v.jwks.expiresAt.IsZero() {
			log.Debug().Msg("Quick Tunnel authentication broker JWKS cache expired")
		}
		return nil, nil
	}
	return findQuickTunnelAuthBrokerVerificationKey(v.jwks.keySet, keyID)
}

// notifyJWKSRefresh asks the refresh worker to update the cache and waits for
// the notification to be processed or for the caller's context to be canceled.
func (v *QuickTunnelAuthAssertionValidator) notifyJWKSRefresh(ctx context.Context, keyID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	notification := quickTunnelAuthBrokerJWKSRefreshNotification{
		keyID:  keyID,
		result: make(chan error, 1),
	}

	select {
	case v.refreshNotifications <- notification:
	case <-v.refreshWorkerStopped:
		return errQuickTunnelAuthBrokerJWKSRefreshWorkerStopped
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-notification.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runJWKSRefreshWorker serializes refresh notifications so concurrent cache
// misses share one fetched key set.
func (v *QuickTunnelAuthAssertionValidator) runJWKSRefreshWorker(ctx context.Context) {
	defer close(v.refreshWorkerStopped)

	var lastRefreshAttempt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case notification := <-v.refreshNotifications:
			err := v.refreshJWKS(ctx, notification.keyID, &lastRefreshAttempt)
			notification.result <- err
		}
	}
}

// refreshJWKS refreshes the cache for a missing key while enforcing the broker
// refresh cooldown. It is called only by the refresh worker.
func (v *QuickTunnelAuthAssertionValidator) refreshJWKS(ctx context.Context, keyID string, lastRefreshAttempt *time.Time) error {
	key, err := v.cachedVerificationKey(keyID)
	if err == nil && key != nil {
		return nil
	}

	now := v.now()
	if !lastRefreshAttempt.IsZero() && now.Before(lastRefreshAttempt.Add(quickTunnelAuthBrokerJWKSRefreshCooldown)) {
		return errors.New("broker JWKS refresh is rate limited")
	}
	*lastRefreshAttempt = now

	refreshCtx, cancel := context.WithTimeout(ctx, quickTunnelAuthBrokerJWKSTimeout)
	defer cancel()
	keySet, err := v.fetchJWKSWithRetry(refreshCtx)
	if err != nil {
		return fmt.Errorf("fetch broker JWKS: %w", err)
	}

	v.jwks.mu.Lock()
	v.jwks.keySet = *keySet
	v.jwks.expiresAt = v.now().Add(quickTunnelAuthBrokerJWKSCacheTTL)
	v.jwks.mu.Unlock()

	key, err = findQuickTunnelAuthBrokerVerificationKey(*keySet, keyID)
	if err != nil {
		return err
	}
	if key == nil {
		return errors.New("broker assertion verification key is unavailable after JWKS refresh")
	}
	return nil
}

// fetchJWKSWithRetry retries transient broker failures with bounded exponential
// backoff. The final error is returned through the refresh notification so the
// caller can handle and log it once.
func (v *QuickTunnelAuthAssertionValidator) fetchJWKSWithRetry(ctx context.Context) (*jose.JSONWebKeySet, error) {
	backoff := quickTunnelAuthBrokerJWKSInitialRetryBackoff
	var lastErr error
	for attempt := 1; attempt <= quickTunnelAuthBrokerJWKSMaxFetchAttempts; attempt++ {
		keySet, err := v.fetchJWKS(ctx)
		if err == nil {
			return keySet, nil
		}
		lastErr = err
		if !isRetryableQuickTunnelAuthBrokerJWKSError(err) {
			return nil, err
		}
		if attempt < quickTunnelAuthBrokerJWKSMaxFetchAttempts {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return nil, ctx.Err()
			case <-timer.C:
			}
			// Double the delay after each failed attempt for exponential backoff.
			backoff *= 2
		}
	}
	return nil, lastErr
}

// fetchJWKS retrieves and validates the broker key set from the configured
// JWKS endpoint.
func (v *QuickTunnelAuthAssertionValidator) fetchJWKS(ctx context.Context) (*jose.JSONWebKeySet, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create broker JWKS request: %w", err)
	}

	response, err := v.httpClient.Do(request) //nolint:gosec // Production uses the fixed broker endpoint; tests inject local JWKS servers.
	if err != nil {
		return nil, &retryableQuickTunnelAuthBrokerJWKSError{err: fmt.Errorf("request broker JWKS: %w", err)}
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("broker JWKS endpoint returned status %d", response.StatusCode)
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
			return nil, &retryableQuickTunnelAuthBrokerJWKSError{err: err}
		}
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, quickTunnelAuthBrokerMaxJWKSResponseSize+1))
	if err != nil {
		return nil, &retryableQuickTunnelAuthBrokerJWKSError{err: fmt.Errorf("read broker JWKS response: %w", err)}
	}
	if len(body) > quickTunnelAuthBrokerMaxJWKSResponseSize {
		return nil, fmt.Errorf("broker JWKS response exceeds %d bytes", quickTunnelAuthBrokerMaxJWKSResponseSize)
	}

	var keySet jose.JSONWebKeySet
	if err := json.Unmarshal(body, &keySet); err != nil {
		return nil, fmt.Errorf("parse broker JWKS response: %w", err)
	}
	if err := validateQuickTunnelAuthBrokerJWKS(&keySet); err != nil {
		return nil, err
	}
	return &keySet, nil
}

// validateQuickTunnelAuthBrokerJWKS requires a non-empty key set containing
// unique key IDs and supported verification keys.
func validateQuickTunnelAuthBrokerJWKS(keySet *jose.JSONWebKeySet) error {
	if keySet == nil || len(keySet.Keys) == 0 {
		return errors.New("broker JWKS contains no verification keys")
	}

	keyIDs := make(map[string]struct{}, len(keySet.Keys))
	for index := range keySet.Keys {
		key := &keySet.Keys[index]
		if err := validateQuickTunnelAuthBrokerKeyID(key.KeyID); err != nil {
			return fmt.Errorf("broker JWKS contains an invalid key ID: %w", err)
		}
		if _, exists := keyIDs[key.KeyID]; exists {
			return errors.New("broker JWKS contains duplicate key IDs")
		}
		if err := validateQuickTunnelAuthBrokerVerificationKey(key); err != nil {
			return err
		}

		keyIDs[key.KeyID] = struct{}{}
	}
	return nil
}

// validateQuickTunnelAuthBrokerKeyID bounds and normalizes opaque broker key
// identifiers from assertions and key sets.
func validateQuickTunnelAuthBrokerKeyID(keyID string) error {
	switch {
	case keyID == "":
		return errors.New("cannot be empty")
	case len(keyID) > quickTunnelAuthBrokerMaxKeyIDLength:
		return fmt.Errorf("exceeds %d bytes", quickTunnelAuthBrokerMaxKeyIDLength)
	case strings.TrimSpace(keyID) != keyID:
		return errors.New("cannot contain leading or trailing whitespace")
	default:
		return nil
	}
}

func isRetryableQuickTunnelAuthBrokerJWKSError(err error) bool {
	var retryableError *retryableQuickTunnelAuthBrokerJWKSError
	return errors.As(err, &retryableError)
}

// findQuickTunnelAuthBrokerVerificationKey returns the single key matching
// keyID, or nil when the key set has no match. Callers that pass the shared
// cached key set must hold its read or write lock.
func findQuickTunnelAuthBrokerVerificationKey(keySet jose.JSONWebKeySet, keyID string) (*jose.JSONWebKey, error) {
	matchingKeys := keySet.Key(keyID)
	if len(matchingKeys) == 0 {
		return nil, nil
	}
	if len(matchingKeys) != 1 {
		return nil, errors.New("broker assertion key ID identifies multiple verification keys")
	}

	key := matchingKeys[0]
	return &key, nil
}

// validateQuickTunnelAuthBrokerVerificationKey requires the broker profile's
// explicit alg=ES256 and use=sig metadata as well as a public P-256 key.
func validateQuickTunnelAuthBrokerVerificationKey(key *jose.JSONWebKey) error {
	if key == nil || !key.Valid() || !key.IsPublic() {
		return errors.New("broker JWKS contains an invalid verification key")
	}
	if key.Algorithm != string(jose.ES256) || key.Use != quickTunnelAuthBrokerKeyUse {
		return errors.New("broker JWKS contains a key with unsupported metadata")
	}

	publicKey, ok := key.Key.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P256() {
		return errors.New("broker JWKS contains a key that is not an ES256 public key")
	}
	return nil
}
