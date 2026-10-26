package quicktunnelauth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateQuickTunnelReturnPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawPath  string
		expected string
	}{
		{
			name:     "root path",
			rawPath:  "/",
			expected: "/",
		},
		{
			name:     "nested path",
			rawPath:  "/dashboard/logs",
			expected: "/dashboard/logs",
		},
		{
			name:     "path with query",
			rawPath:  "/dashboard?tab=logs&page=2",
			expected: "/dashboard?tab=logs&page=2",
		},
		{
			name:     "escaped path",
			rawPath:  "/dashboard/my%20logs",
			expected: "/dashboard/my%20logs",
		},
		{
			name:     "unescaped Unicode path",
			rawPath:  "/café",
			expected: "/caf%C3%A9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			actual, err := validateQuickTunnelReturnPath(test.rawPath)
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestBeginQuickTunnelLogin(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	request := httptest.NewRequest(
		http.MethodGet,
		"https://test-tunnel.trycloudflare.com/dashboard?tab=logs",
		nil,
	)

	login, err := manager.BeginLogin(request)
	require.NoError(t, err)
	require.NotNil(t, login)

	decodedState, err := base64.RawURLEncoding.DecodeString(login.State)
	require.NoError(t, err)
	assert.Equal(t, bytes.Repeat([]byte{1}, quickTunnelAuthStateSize), decodedState)

	require.NotNil(t, login.RedirectURL)
	assert.Equal(t, "https", login.RedirectURL.Scheme)
	assert.Equal(t, "login.trycloudflare.com", login.RedirectURL.Host)
	assert.Equal(t, "/authorize", login.RedirectURL.Path)
	assert.Equal(t, map[string][]string{
		"hostname": {"test-tunnel.trycloudflare.com"},
		"state":    {login.State},
	}, map[string][]string(login.RedirectURL.Query()))

	require.NotNil(t, login.Cookie)
	assert.Equal(t, quickTunnelAuthStateCookieName(login.State), login.Cookie.Name)
	assert.Equal(t, QuickTunnelAuthCallbackPath, login.Cookie.Path)
	assert.Equal(t, testQuickTunnelAuthNow.Add(QuickTunnelAuthStateTTL), login.Cookie.Expires)
	assert.Equal(t, int(QuickTunnelAuthStateTTL/time.Second), login.Cookie.MaxAge)
	assert.True(t, login.Cookie.Secure)
	assert.True(t, login.Cookie.HttpOnly)
	assert.Equal(t, http.SameSiteNoneMode, login.Cookie.SameSite)
	assert.Empty(t, login.Cookie.Domain)
	assert.LessOrEqual(t, len(login.Cookie.String()), quickTunnelAuthMaxStateCookieBytes)
	assertValidQuickTunnelAuthStateCookie(
		t,
		manager,
		login.Cookie,
		login.State,
		"test-tunnel.trycloudflare.com",
		"/dashboard?tab=logs",
	)
}

func TestBeginQuickTunnelLoginDoesNotTrackStateCollisions(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	manager.random = bytes.NewReader(bytes.Repeat([]byte{1}, quickTunnelAuthStateSize*2))

	firstRequest := httptest.NewRequest(
		http.MethodGet,
		"https://test-tunnel.trycloudflare.com/first",
		nil,
	)
	firstLogin, err := manager.BeginLogin(firstRequest)
	require.NoError(t, err)

	secondRequest := httptest.NewRequest(
		http.MethodGet,
		"https://test-tunnel.trycloudflare.com/second",
		nil,
	)
	secondLogin, err := manager.BeginLogin(secondRequest)
	require.NoError(t, err)

	assert.Equal(t, firstLogin.State, secondLogin.State)
	assert.NotEqual(t, firstLogin.Cookie.Value, secondLogin.Cookie.Value)
	assertValidQuickTunnelAuthStateCookie(
		t,
		manager,
		firstLogin.Cookie,
		firstLogin.State,
		"test-tunnel.trycloudflare.com",
		"/first",
	)
	assertValidQuickTunnelAuthStateCookie(
		t,
		manager,
		secondLogin.Cookie,
		secondLogin.State,
		"test-tunnel.trycloudflare.com",
		"/second",
	)
}

func TestBeginQuickTunnelLoginAcceptsHead(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	request := httptest.NewRequest(
		http.MethodHead,
		"https://test-tunnel.trycloudflare.com/",
		nil,
	)

	login, err := manager.BeginLogin(request)
	require.NoError(t, err)
	require.NotNil(t, login)
}

func TestBeginQuickTunnelLoginAcceptsExplicitHTTPSPort(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	request := httptest.NewRequest(
		http.MethodGet,
		"https://test-tunnel.trycloudflare.com:443/",
		nil,
	)

	login, err := manager.BeginLogin(request)
	require.NoError(t, err)
	require.NotNil(t, login)
}

func TestMatchesQuickTunnelRequestHostname(t *testing.T) {
	t.Parallel()

	const hostname = "test-tunnel.trycloudflare.com"
	tests := []struct {
		name      string
		authority string
		matches   bool
	}{
		{name: "without port", authority: hostname, matches: true},
		{name: "canonical HTTPS port", authority: hostname + ":443", matches: true},
		{name: "HTTPS port with leading zero", authority: hostname + ":0443", matches: true},
		{name: "unexpected port", authority: hostname + ":8443"},
		{name: "non-numeric port", authority: hostname + ":https"},
		{name: "out-of-range port", authority: hostname + ":65536"},
		{name: "different hostname", authority: "other.trycloudflare.com"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.matches, matchesQuickTunnelRequestHostname(test.authority, hostname))
		})
	}
}

func TestBeginQuickTunnelLoginRejectsInvalidRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		build      func() *http.Request
		errMessage string
	}{
		{
			name:       "nil request",
			build:      func() *http.Request { return nil },
			errMessage: "request cannot be nil",
		},
		{
			name: "nil URL",
			build: func() *http.Request {
				return &http.Request{
					Method: http.MethodGet,
					Host:   "test-tunnel.trycloudflare.com",
					URL:    nil,
				}
			},
			errMessage: "request URL cannot be nil",
		},
		{
			name: "unsupported method",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "https://test-tunnel.trycloudflare.com/", nil)
			},
			errMessage: "only GET and HEAD are allowed",
		},
		{
			name: "wrong hostname",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "https://other.trycloudflare.com/", nil)
			},
			errMessage: "does not match protected Quick Tunnel hostname",
		},
		{
			name: "unexpected port",
			build: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "https://test-tunnel.trycloudflare.com:8443/", nil)
			},
			errMessage: "does not match protected Quick Tunnel hostname",
		},
		{
			name: "callback return path",
			build: func() *http.Request {
				return httptest.NewRequest(
					http.MethodGet,
					"https://test-tunnel.trycloudflare.com"+QuickTunnelAuthCallbackPath,
					nil,
				)
			},
			errMessage: "validate authentication return path",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestQuickTunnelAuthStateManager(t)
			login, err := manager.BeginLogin(test.build())
			assert.Nil(t, login)
			require.ErrorContains(t, err, test.errMessage)
		})
	}
}

func TestBeginQuickTunnelLoginRejectsRandomFailure(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	manager.random = strings.NewReader("")
	request := httptest.NewRequest(
		http.MethodGet,
		"https://test-tunnel.trycloudflare.com/",
		nil,
	)

	login, err := manager.BeginLogin(request)
	assert.Nil(t, login)
	require.ErrorContains(t, err, "generate authentication state")
}

func TestBeginQuickTunnelLoginRejectsOversizedStateCookie(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	request := httptest.NewRequest(
		http.MethodGet,
		"https://test-tunnel.trycloudflare.com/"+strings.Repeat("a", quickTunnelAuthMaxStateCookieBytes),
		nil,
	)

	login, err := manager.BeginLogin(request)
	assert.Nil(t, login)
	require.ErrorContains(t, err, "authentication-state cookie exceeds")
}

func TestValidateQuickTunnelReturnPathRejectsUnsafeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rawPath string
	}{
		{name: "empty", rawPath: ""},
		{name: "relative path", rawPath: "dashboard"},
		{name: "absolute URL", rawPath: "https://example.com/dashboard"},
		{name: "network path", rawPath: "//example.com/dashboard"},
		{name: "encoded network path", rawPath: "/%2F%2Fexample.com/dashboard"},
		{name: "backslash", rawPath: `/dashboard\logs`},
		{name: "encoded backslash", rawPath: "/dashboard%5Clogs"},
		{name: "callback path", rawPath: QuickTunnelAuthCallbackPath},
		{name: "callback path with query", rawPath: QuickTunnelAuthCallbackPath + "?state=value"},
		{name: "malformed escape", rawPath: "/dashboard%zz"},
		{name: "fragment", rawPath: "/dashboard#logs"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			actual, err := validateQuickTunnelReturnPath(test.rawPath)
			assert.Empty(t, actual)
			require.ErrorIs(t, err, errInvalidQuickTunnelReturnPath)
		})
	}
}

func TestIsQuickTunnelHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hostname string
		valid    bool
	}{
		{name: "valid", hostname: "test-tunnel.trycloudflare.com", valid: true},
		{name: "valid with digits", hostname: "test-123.trycloudflare.com", valid: true},
		{name: "uppercase", hostname: "Test-Tunnel.trycloudflare.com", valid: false},
		{name: "empty label", hostname: ".trycloudflare.com", valid: false},
		{name: "starts with dash", hostname: "-test.trycloudflare.com", valid: false},
		{name: "ends with dash", hostname: "test-.trycloudflare.com", valid: false},
		{name: "non-ascii letter", hostname: "tëst.trycloudflare.com", valid: false},
		{name: "underscore", hostname: "test_tunnel.trycloudflare.com", valid: false},
		{name: "too long label", hostname: strings.Repeat("a", 64) + ".trycloudflare.com", valid: false},
		{name: "wrong suffix", hostname: "test-tunnel.example.com", valid: false},
		{name: "extra labels", hostname: "test-tunnel.foo.trycloudflare.com", valid: false},
		{name: "missing suffix", hostname: "test-tunnel", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.valid, isQuickTunnelHostname(test.hostname))
		})
	}
}

var testQuickTunnelAuthNow = time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)

func newTestQuickTunnelAuthStateManager(t *testing.T) *QuickTunnelAuthStateManager {
	t.Helper()

	manager, err := NewQuickTunnelAuthStateManager("test-tunnel.trycloudflare.com")
	require.NoError(t, err)

	manager.signingKey = bytes.Repeat([]byte{2}, quickTunnelAuthSigningKeySize)
	manager.random = bytes.NewReader(bytes.Repeat([]byte{1}, quickTunnelAuthStateSize))
	manager.now = func() time.Time { return testQuickTunnelAuthNow }
	return manager
}

func assertValidQuickTunnelAuthStateCookie(
	t *testing.T,
	manager *QuickTunnelAuthStateManager,
	cookie *http.Cookie,
	expectedState string,
	expectedHostname string,
	expectedReturnPath string,
) {
	t.Helper()

	encodedPayload, encodedSignature, ok := strings.Cut(cookie.Value, ".")
	require.True(t, ok)

	payloadBytes, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	require.NoError(t, err)
	var payload quickTunnelAuthStateCookiePayload
	require.NoError(t, json.Unmarshal(payloadBytes, &payload))
	assert.Equal(t, expectedState, payload.State)
	assert.Equal(t, expectedHostname, payload.Hostname)
	assert.Equal(t, expectedReturnPath, payload.ReturnPath)
	assert.Equal(t, cookie.Expires.Unix(), payload.ExpiresAt)

	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, manager.signingKey)
	_, err = mac.Write([]byte(encodedPayload))
	require.NoError(t, err)
	assert.True(t, hmac.Equal(mac.Sum(nil), signature))
}
