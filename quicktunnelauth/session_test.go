package quicktunnelauth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQuickTunnelAuthSessionManager(t *testing.T) {
	t.Parallel()

	first, err := NewQuickTunnelAuthSessionManager()
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Len(t, first.sessionCookieSigningKey, quickTunnelAuthSessionSigningKeySize)
	assert.NotNil(t, first.random)
	assert.NotNil(t, first.now)

	second, err := NewQuickTunnelAuthSessionManager()
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.NotEqual(t, first.sessionCookieSigningKey, second.sessionCookieSigningKey)
}

func TestQuickTunnelAuthSessionManagerSign(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x42}, quickTunnelAuthSessionSigningKeySize)
	manager := &QuickTunnelAuthSessionManager{sessionCookieSigningKey: key}
	payload := []byte("session payload")

	signature, err := manager.sign(payload)
	require.NoError(t, err)

	expectedMAC := hmac.New(sha256.New, key)
	_, err = expectedMAC.Write(payload)
	require.NoError(t, err)
	assert.Equal(t, expectedMAC.Sum(nil), signature)
	assert.Len(t, signature, sha256.Size)
}

func TestQuickTunnelAuthSessionManagerSignRequiresKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		manager *QuickTunnelAuthSessionManager
	}{
		{name: "nil manager"},
		{name: "empty key", manager: &QuickTunnelAuthSessionManager{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := test.manager.sign([]byte("payload"))
			require.EqualError(t, err, "session cookie signing key is not initialized")
		})
	}
}

func TestQuickTunnelAuthSessionManagerIssueSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name              string
		identityExpiresAt time.Time
		expectedExpiresAt time.Time
	}{
		{
			name:              "identity expires first",
			identityExpiresAt: now.Add(2 * time.Hour),
			expectedExpiresAt: now.Add(2 * time.Hour),
		},
		{
			name:              "session TTL expires first",
			identityExpiresAt: now.Add(48 * time.Hour),
			expectedExpiresAt: now.Add(4 * time.Hour),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			randomBytes := bytes.Repeat([]byte{0x24}, quickTunnelAuthSessionNonceSize)
			manager := newTestQuickTunnelAuthSessionManager(now, 0x42, bytes.NewReader(randomBytes))

			cookie, err := manager.IssueSession(test.identityExpiresAt)
			require.NoError(t, err)
			require.NotNil(t, cookie)
			assert.Equal(t, quickTunnelAuthSessionCookieName, cookie.Name)
			assert.Equal(t, quickTunnelAuthSessionCookiePath, cookie.Path)
			assert.Empty(t, cookie.Domain)
			assert.True(t, cookie.Secure)
			assert.True(t, cookie.HttpOnly)
			assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
			assert.True(t, cookie.Expires.Equal(test.expectedExpiresAt))

			payloadBytes, signature, payload := decodeQuickTunnelAuthSessionCookie(t, cookie)
			assert.Equal(t, base64.RawURLEncoding.EncodeToString(randomBytes), payload.Nonce)
			assert.Equal(t, test.expectedExpiresAt.Unix(), payload.ExpiresAt)

			expectedSignature, err := manager.sign(payloadBytes)
			require.NoError(t, err)
			assert.True(t, hmac.Equal(expectedSignature, signature))
		})
	}
}

func TestQuickTunnelAuthSessionManagerIssueSessionRejectsExpiredIdentity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name              string
		identityExpiresAt time.Time
	}{
		{name: "at current time", identityExpiresAt: now},
		{name: "before current time", identityExpiresAt: now.Add(-time.Second)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestQuickTunnelAuthSessionManager(now, 0x42, bytes.NewReader(make([]byte, quickTunnelAuthSessionNonceSize)))
			cookie, err := manager.IssueSession(test.identityExpiresAt)
			require.EqualError(t, err, "issue authentication session: identity has expired")
			assert.Nil(t, cookie)
		})
	}
}

func TestQuickTunnelAuthSessionManagerIssueSessionRejectsIdentityExpiringWithinCurrentSecond(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC).Add(100 * time.Millisecond)
	manager := newTestQuickTunnelAuthSessionManager(
		now,
		0x42,
		bytes.NewReader(make([]byte, quickTunnelAuthSessionNonceSize)),
	)

	cookie, err := manager.IssueSession(now.Add(500 * time.Millisecond))
	require.EqualError(t, err, "issue authentication session: identity expires too soon")
	assert.Nil(t, cookie)
}

func TestQuickTunnelAuthSessionManagerIssueSessionRequiresInitializedManager(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		manager *QuickTunnelAuthSessionManager
	}{
		{name: "nil manager"},
		{name: "zero-value manager", manager: &QuickTunnelAuthSessionManager{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cookie, err := test.manager.IssueSession(now.Add(time.Hour))
			require.EqualError(t, err, "authentication session manager is not initialized")
			assert.Nil(t, cookie)
		})
	}
}

func TestQuickTunnelAuthSessionManagerIssueSessionErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)

	t.Run("random source failure", func(t *testing.T) {
		t.Parallel()

		manager := newTestQuickTunnelAuthSessionManager(now, 0x42, iotest.ErrReader(assert.AnError))
		cookie, err := manager.IssueSession(now.Add(time.Hour))
		require.EqualError(t, err, "generate authentication session nonce: assert.AnError general error for testing")
		assert.Nil(t, cookie)
	})

	t.Run("missing signing key", func(t *testing.T) {
		t.Parallel()

		manager := newTestQuickTunnelAuthSessionManager(now, 0, bytes.NewReader(make([]byte, quickTunnelAuthSessionNonceSize)))
		manager.sessionCookieSigningKey = nil
		cookie, err := manager.IssueSession(now.Add(time.Hour))
		require.EqualError(t, err, "issue authentication session: session cookie signing key is not initialized")
		assert.Nil(t, cookie)
	})
}

func TestQuickTunnelAuthSessionManagerValidateSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	manager := newTestQuickTunnelAuthSessionManager(
		now,
		0x42,
		bytes.NewReader(bytes.Repeat([]byte{0x24}, quickTunnelAuthSessionNonceSize)),
	)
	cookie, err := manager.IssueSession(now.Add(time.Hour))
	require.NoError(t, err)

	request := requestWithQuickTunnelAuthSession(cookie.Value)
	valid, err := manager.ValidateSession(request)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestQuickTunnelAuthSessionManagerValidateSessionRejectsDuplicateCookies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	manager := newTestQuickTunnelAuthSessionManager(
		now,
		0x42,
		bytes.NewReader(bytes.Repeat([]byte{0x24}, quickTunnelAuthSessionNonceSize)),
	)
	cookie, err := manager.IssueSession(now.Add(time.Hour))
	require.NoError(t, err)

	request := requestWithQuickTunnelAuthSession(cookie.Value)
	request.AddCookie(cookie)

	valid, err := manager.ValidateSession(request)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestQuickTunnelAuthSessionManagerValidateSessionRemovesSessionCookie(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	manager := newTestQuickTunnelAuthSessionManager(
		now,
		0x42,
		bytes.NewReader(bytes.Repeat([]byte{0x24}, quickTunnelAuthSessionNonceSize)),
	)
	cookie, err := manager.IssueSession(now.Add(time.Hour))
	require.NoError(t, err)

	request := requestWithQuickTunnelAuthSession(cookie.Value)
	request.AddCookie(&http.Cookie{
		Name:     "origin-session",
		Value:    "origin-value",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	valid, err := manager.ValidateSession(request)
	require.NoError(t, err)
	assert.True(t, valid)
	assert.Empty(t, request.CookiesNamed(quickTunnelAuthSessionCookieName))

	originCookies := request.CookiesNamed("origin-session")
	require.Len(t, originCookies, 1)
	assert.Equal(t, "origin-value", originCookies[0].Value)
}

func TestQuickTunnelAuthSessionManagerValidateSessionExpirationBoundaries(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, time.September, 3, 10, 0, 0, 0, time.UTC)
	validNonce := base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthSessionNonceSize))
	tests := []struct {
		name      string
		now       time.Time
		expiresAt time.Time
		valid     bool
	}{
		{name: "immediately before expiry", now: expiresAt.Add(-time.Nanosecond), expiresAt: expiresAt, valid: true},
		{name: "at expiry", now: expiresAt, expiresAt: expiresAt},
		{name: "after expiry", now: expiresAt.Add(time.Nanosecond), expiresAt: expiresAt},
		{name: "at maximum TTL", now: expiresAt, expiresAt: expiresAt.Add(quickTunnelAuthSessionTTL), valid: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestQuickTunnelAuthSessionManager(test.now, 0x42, bytes.NewReader(nil))
			payloadBytes, err := json.Marshal(quickTunnelAuthSessionCookiePayload{
				Nonce:     validNonce,
				ExpiresAt: test.expiresAt.Unix(),
			})
			require.NoError(t, err)
			cookie := signedQuickTunnelAuthSessionCookie(t, manager, payloadBytes)

			valid, err := manager.ValidateSession(requestWithQuickTunnelAuthSession(cookie.Value))
			require.NoError(t, err)
			assert.Equal(t, test.valid, valid)
		})
	}
}

func TestQuickTunnelAuthSessionManagerValidateSessionRejectsMalformedCookies(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	manager := newTestQuickTunnelAuthSessionManager(now, 0x42, bytes.NewReader(nil))
	tests := []struct {
		name        string
		cookieValue string
	}{
		{name: "empty"},
		{name: "missing separator", cookieValue: "payload"},
		{name: "empty payload", cookieValue: ".signature"},
		{name: "empty signature", cookieValue: "payload."},
		{name: "extra separator", cookieValue: "payload.signature.extra"},
		{name: "invalid payload encoding", cookieValue: "*.AA"},
		{name: "invalid signature encoding", cookieValue: "e30.!"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			valid, err := manager.ValidateSession(requestWithQuickTunnelAuthSession(test.cookieValue))
			require.NoError(t, err)
			assert.False(t, valid)
		})
	}
}

func TestQuickTunnelAuthSessionManagerValidateSessionRejectsInvalidPayloads(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	validNonce := base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthSessionNonceSize))
	nonCanonicalNonce := validNonce[:len(validNonce)-1] + "B"
	tests := []struct {
		name       string
		rawPayload []byte
		payload    *quickTunnelAuthSessionCookiePayload
	}{
		{name: "invalid JSON", rawPayload: []byte("{")},
		{
			name: "invalid nonce encoding",
			payload: &quickTunnelAuthSessionCookiePayload{
				Nonce:     "not-base64!",
				ExpiresAt: now.Add(time.Hour).Unix(),
			},
		},
		{
			name: "invalid nonce size",
			payload: &quickTunnelAuthSessionCookiePayload{
				Nonce:     base64.RawURLEncoding.EncodeToString(make([]byte, quickTunnelAuthSessionNonceSize-1)),
				ExpiresAt: now.Add(time.Hour).Unix(),
			},
		},
		{
			name: "non-canonical nonce",
			payload: &quickTunnelAuthSessionCookiePayload{
				Nonce:     nonCanonicalNonce,
				ExpiresAt: now.Add(time.Hour).Unix(),
			},
		},
		{
			name: "expired at boundary",
			payload: &quickTunnelAuthSessionCookiePayload{
				Nonce:     validNonce,
				ExpiresAt: now.Unix(),
			},
		},
		{
			name: "expired before boundary",
			payload: &quickTunnelAuthSessionCookiePayload{
				Nonce:     validNonce,
				ExpiresAt: now.Add(-time.Second).Unix(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestQuickTunnelAuthSessionManager(now, 0x42, bytes.NewReader(nil))
			payloadBytes := test.rawPayload
			if test.payload != nil {
				var err error
				payloadBytes, err = json.Marshal(test.payload)
				require.NoError(t, err)
			}
			cookie := signedQuickTunnelAuthSessionCookie(t, manager, payloadBytes)

			valid, err := manager.ValidateSession(requestWithQuickTunnelAuthSession(cookie.Value))
			require.NoError(t, err)
			assert.False(t, valid)
		})
	}
}

func TestQuickTunnelAuthSessionManagerValidateSessionRejectsTampering(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		tamperPayload   bool
		tamperSignature bool
	}{
		{name: "payload", tamperPayload: true},
		{name: "signature", tamperSignature: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestQuickTunnelAuthSessionManager(
				now,
				0x42,
				bytes.NewReader(bytes.Repeat([]byte{0x24}, quickTunnelAuthSessionNonceSize)),
			)
			cookie, err := manager.IssueSession(now.Add(time.Hour))
			require.NoError(t, err)

			payloadBytes, signature, _ := decodeQuickTunnelAuthSessionCookie(t, cookie)
			if test.tamperPayload {
				payloadBytes[0] ^= 0xff
			}
			if test.tamperSignature {
				signature[0] ^= 0xff
			}
			cookie.Value = encodeQuickTunnelAuthSessionCookieValue(payloadBytes, signature)

			valid, err := manager.ValidateSession(requestWithQuickTunnelAuthSession(cookie.Value))
			require.NoError(t, err)
			assert.False(t, valid)
		})
	}
}

func TestQuickTunnelAuthSessionManagerValidateSessionRejectsPreviousProcess(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	firstManager := newTestQuickTunnelAuthSessionManager(
		now,
		0x11,
		bytes.NewReader(bytes.Repeat([]byte{0x24}, quickTunnelAuthSessionNonceSize)),
	)
	cookie, err := firstManager.IssueSession(now.Add(time.Hour))
	require.NoError(t, err)

	restartedManager := newTestQuickTunnelAuthSessionManager(now, 0x22, bytes.NewReader(nil))
	valid, err := restartedManager.ValidateSession(requestWithQuickTunnelAuthSession(cookie.Value))
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestQuickTunnelAuthSessionManagerValidateSessionErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC)
	manager := newTestQuickTunnelAuthSessionManager(now, 0x42, bytes.NewReader(nil))

	t.Run("nil request", func(t *testing.T) {
		t.Parallel()

		valid, err := manager.ValidateSession(nil)
		require.EqualError(t, err, "validate authentication session: request is nil")
		assert.False(t, valid)
	})

	t.Run("missing cookie", func(t *testing.T) {
		t.Parallel()

		request := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
		valid, err := manager.ValidateSession(request)
		require.NoError(t, err)
		assert.False(t, valid)
	})

	t.Run("uninitialized manager", func(t *testing.T) {
		t.Parallel()

		var uninitialized *QuickTunnelAuthSessionManager
		valid, err := uninitialized.ValidateSession(httptest.NewRequest(http.MethodGet, "https://example.com", nil))
		require.EqualError(t, err, "authentication session manager is not initialized")
		assert.False(t, valid)
	})

	t.Run("missing signing key", func(t *testing.T) {
		t.Parallel()

		unsignedManager := newTestQuickTunnelAuthSessionManager(now, 0, bytes.NewReader(nil))
		unsignedManager.sessionCookieSigningKey = nil
		payloadBytes := []byte(`{}`)
		signature := make([]byte, sha256.Size)
		request := requestWithQuickTunnelAuthSession(encodeQuickTunnelAuthSessionCookieValue(payloadBytes, signature))

		valid, err := unsignedManager.ValidateSession(request)
		require.EqualError(t, err, "verify authentication session signature: session cookie signing key is not initialized")
		assert.False(t, valid)
	})
}

func newTestQuickTunnelAuthSessionManager(now time.Time, keyByte byte, random io.Reader) *QuickTunnelAuthSessionManager {
	return &QuickTunnelAuthSessionManager{
		sessionCookieSigningKey: bytes.Repeat([]byte{keyByte}, quickTunnelAuthSessionSigningKeySize),
		random:                  random,
		now:                     func() time.Time { return now },
	}
}

func signedQuickTunnelAuthSessionCookie(t *testing.T, manager *QuickTunnelAuthSessionManager, payloadBytes []byte) *http.Cookie {
	t.Helper()

	signature, err := manager.sign(payloadBytes)
	require.NoError(t, err)
	return &http.Cookie{
		Name:     quickTunnelAuthSessionCookieName,
		Value:    encodeQuickTunnelAuthSessionCookieValue(payloadBytes, signature),
		Path:     quickTunnelAuthSessionCookiePath,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}

func decodeQuickTunnelAuthSessionCookie(t *testing.T, cookie *http.Cookie) ([]byte, []byte, quickTunnelAuthSessionCookiePayload) {
	t.Helper()

	encodedPayload, encodedSignature, found := strings.Cut(cookie.Value, quickTunnelAuthSessionCookieSeparator)
	require.True(t, found)
	payloadBytes, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	require.NoError(t, err)
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	require.NoError(t, err)

	var payload quickTunnelAuthSessionCookiePayload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))
	return payloadBytes, signature, payload
}

func requestWithQuickTunnelAuthSession(cookieValue string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	request.AddCookie(&http.Cookie{
		Name:     quickTunnelAuthSessionCookieName,
		Value:    cookieValue,
		Path:     quickTunnelAuthSessionCookiePath,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return request
}
