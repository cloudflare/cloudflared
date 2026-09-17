package quicktunnelauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// quickTunnelAuthSessionSigningKeySize provides a 256-bit HMAC key for
	// signing authentication session cookies.
	quickTunnelAuthSessionSigningKeySize = 32
	quickTunnelAuthSessionTTL            = 4 * time.Hour
	quickTunnelAuthSessionNonceSize      = 16 // 128 bits
	// __Host- makes the browser require Secure, Path=/, and no Domain attribute,
	// binding the session cookie to one exact Quick Tunnel hostname.
	// See https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Cookies#cookie_prefixes.
	quickTunnelAuthSessionCookieName      = "__Host-cloudflared-qt-auth-session"
	quickTunnelAuthSessionCookiePath      = "/"
	quickTunnelAuthSessionCookieSeparator = "."
)

type quickTunnelAuthTimeSource func() time.Time

// QuickTunnelAuthSessionManager manages process-local authentication sessions.
// It must be created with NewQuickTunnelAuthSessionManager.
type QuickTunnelAuthSessionManager struct {
	sessionCookieSigningKey []byte
	random                  io.Reader
	// now is injectable so expiry behavior can be tested with a deterministic clock.
	now quickTunnelAuthTimeSource
}

// quickTunnelAuthSessionCookiePayload is the signed process-local session data
// stored in the browser cookie. It intentionally contains no visitor identity
// or recipient rules.
type quickTunnelAuthSessionCookiePayload struct {
	Nonce     string `json:"nonce"`
	ExpiresAt int64  `json:"exp"`
}

// NewQuickTunnelAuthSessionManager creates a session manager with a fresh
// process-local signing key, invalidating sessions when cloudflared restarts.
func NewQuickTunnelAuthSessionManager() (*QuickTunnelAuthSessionManager, error) {
	key := make([]byte, quickTunnelAuthSessionSigningKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate session cookie signing key: %w", err)
	}

	return &QuickTunnelAuthSessionManager{
		sessionCookieSigningKey: key,
		random:                  rand.Reader,
		now:                     time.Now,
	}, nil
}

// sign returns an HMAC-SHA256 signature over payload using the manager's
// process-local signing key.
func (m *QuickTunnelAuthSessionManager) sign(payload []byte) ([]byte, error) {
	if m == nil || len(m.sessionCookieSigningKey) == 0 {
		return nil, errors.New("session cookie signing key is not initialized")
	}

	mac := hmac.New(sha256.New, m.sessionCookieSigningKey)
	if _, err := mac.Write(payload); err != nil {
		return nil, fmt.Errorf("sign session cookie payload: %w", err)
	}

	return mac.Sum(nil), nil
}

// encodeQuickTunnelAuthSessionCookieValue encodes a signed session as
// "<base64url payload>.<base64url HMAC signature>".
func encodeQuickTunnelAuthSessionCookieValue(payloadBytes, signature []byte) string {
	return base64.RawURLEncoding.EncodeToString(payloadBytes) +
		quickTunnelAuthSessionCookieSeparator +
		base64.RawURLEncoding.EncodeToString(signature)
}

// IssueSession creates a process-local authentication session cookie.
func (m *QuickTunnelAuthSessionManager) IssueSession(identityExpiresAt time.Time) (*http.Cookie, error) {
	if m == nil || m.now == nil || m.random == nil {
		return nil, errors.New("authentication session manager is not initialized")
	}

	now := m.now()
	if identityExpiresAt.Compare(now) <= 0 {
		return nil, errors.New("issue authentication session: identity has expired")
	}

	expiresAt := min(identityExpiresAt.Unix(), now.Add(quickTunnelAuthSessionTTL).Unix())
	if expiresAt <= now.Unix() {
		return nil, errors.New("issue authentication session: identity expires too soon")
	}

	nonceBytes := make([]byte, quickTunnelAuthSessionNonceSize)
	if _, err := io.ReadFull(m.random, nonceBytes); err != nil {
		return nil, fmt.Errorf("generate authentication session nonce: %w", err)
	}

	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)

	payload := quickTunnelAuthSessionCookiePayload{
		Nonce:     nonce,
		ExpiresAt: expiresAt,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode authentication session payload: %w", err)
	}

	signature, err := m.sign(payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("issue authentication session: %w", err)
	}

	cookieValue := encodeQuickTunnelAuthSessionCookieValue(payloadBytes, signature)

	// SameSite=Lax allows an existing session on top-level navigations from
	// external links while withholding it from cross-site subrequests and unsafe
	// methods.
	return &http.Cookie{
		Name:     quickTunnelAuthSessionCookieName,
		Value:    cookieValue,
		Path:     quickTunnelAuthSessionCookiePath,
		Expires:  time.Unix(expiresAt, 0).UTC(),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

// removeQuickTunnelAuthSessionCookie removes the process-local session cookie
// before the request is proxied while preserving unrelated origin cookies.
func removeQuickTunnelAuthSessionCookie(request *http.Request) {
	cookies := request.Cookies()
	request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != quickTunnelAuthSessionCookieName {
			request.AddCookie(cookie)
		}
	}
}

// ValidateSession validates the authentication session attached to request and
// removes a valid session cookie before the request is proxied to the origin.
func (m *QuickTunnelAuthSessionManager) ValidateSession(request *http.Request) (bool, error) {
	if m == nil || m.now == nil {
		return false, errors.New("authentication session manager is not initialized")
	}
	if request == nil {
		return false, errors.New("validate authentication session: request is nil")
	}

	sessionCookies := request.CookiesNamed(quickTunnelAuthSessionCookieName)
	if len(sessionCookies) != 1 {
		return false, nil
	}
	sessionCookie := sessionCookies[0]

	encodedPayload, encodedSignature, found := strings.Cut(sessionCookie.Value, quickTunnelAuthSessionCookieSeparator)
	if !found || encodedPayload == "" || encodedSignature == "" {
		return false, nil
	}

	payloadBytes, err := base64.RawURLEncoding.Strict().DecodeString(encodedPayload)
	if err != nil {
		return false, nil
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(encodedSignature)
	if err != nil {
		return false, nil
	}

	expectedSignature, err := m.sign(payloadBytes)
	if err != nil {
		return false, fmt.Errorf("verify authentication session signature: %w", err)
	}
	if !hmac.Equal(signature, expectedSignature) {
		return false, nil
	}

	var payload quickTunnelAuthSessionCookiePayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return false, nil
	}
	nonceBytes, err := base64.RawURLEncoding.Strict().DecodeString(payload.Nonce)
	if err != nil || len(nonceBytes) != quickTunnelAuthSessionNonceSize {
		return false, nil
	}

	now := m.now()
	expiresAt := time.Unix(payload.ExpiresAt, 0)
	if now.Compare(expiresAt) >= 0 {
		return false, nil
	}

	removeQuickTunnelAuthSessionCookie(request)
	return true, nil
}
