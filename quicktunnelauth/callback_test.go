package quicktunnelauth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsumeQuickTunnelAuthCallback(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	login := beginTestQuickTunnelLogin(t, manager, "/dashboard?tab=logs")
	request := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, "broker.assertion")

	callback, err := manager.ConsumeCallback(request)
	require.NoError(t, err)
	require.NotNil(t, callback)
	assert.Equal(t, login.State, callback.State)
	assert.Equal(t, "broker.assertion", callback.Assertion)
	assert.Equal(t, "/dashboard?tab=logs", callback.ReturnPath)

	require.NotNil(t, callback.ClearCookie)
	assert.Equal(t, quickTunnelAuthStateCookieName, callback.ClearCookie.Name)
	assert.Empty(t, callback.ClearCookie.Value)
	assert.Equal(t, QuickTunnelAuthCallbackPath, callback.ClearCookie.Path)
	assert.Equal(t, -1, callback.ClearCookie.MaxAge)
	assert.True(t, callback.ClearCookie.Expires.Before(testQuickTunnelAuthNow))
	assert.True(t, callback.ClearCookie.Secure)
	assert.True(t, callback.ClearCookie.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, callback.ClearCookie.SameSite)
	assert.Empty(t, callback.ClearCookie.Domain)
}

func TestConsumeQuickTunnelAuthCallbackSendsCookieOnCrossSiteBrokerPost(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	login := beginTestQuickTunnelLogin(t, manager, "/dashboard")

	// Simulate the broker POST-ing the assertion back from login.trycloudflare.com
	// to the tunnel callback path. Because trycloudflare.com is a public suffix,
	// login.trycloudflare.com and test-tunnel.trycloudflare.com are treated as
	// cross-site by browsers. A SameSite=None cookie is required for the cookie
	// to be included in this request.
	request := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, "broker.assertion")
	request.Header.Set("Origin", "https://login.trycloudflare.com")
	request.Header.Set("Sec-Fetch-Site", "cross-site")

	require.NotNil(t, login.Cookie)
	assert.Equal(t, http.SameSiteNoneMode, login.Cookie.SameSite)
	assert.True(t, login.Cookie.Secure)
	assert.True(t, login.Cookie.HttpOnly)

	callback, err := manager.ConsumeCallback(request)
	require.NoError(t, err)
	require.NotNil(t, callback)
	assert.Equal(t, login.State, callback.State)
	assert.Equal(t, "broker.assertion", callback.Assertion)
	assert.Equal(t, "/dashboard", callback.ReturnPath)
}

func TestConsumeQuickTunnelAuthCallbackAcceptsExplicitHTTPSPort(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	login := beginTestQuickTunnelLogin(t, manager, "/")
	request := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, "broker.assertion")
	request.Host = "test-tunnel.trycloudflare.com:443"

	callback, err := manager.ConsumeCallback(request)
	require.NoError(t, err)
	require.NotNil(t, callback)
}

// TestConsumeQuickTunnelAuthCallbackAllowsReplayWithinTTL documents the
// intentional stateless trade-off. Normal browsers delete the state cookie
// after the first response, while a client retaining the callback artifacts can
// replay them until the short-lived broker assertion expires.
func TestConsumeQuickTunnelAuthCallbackAllowsReplayWithinTTL(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	login := beginTestQuickTunnelLogin(t, manager, "/")

	first := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, "broker.assertion")
	_, err := manager.ConsumeCallback(first)
	require.NoError(t, err)

	second := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, "broker.assertion")
	callback, err := manager.ConsumeCallback(second)
	require.NoError(t, err)
	require.NotNil(t, callback)
}

func TestConsumeQuickTunnelAuthCallbackRejectsExpiredState(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	login := beginTestQuickTunnelLogin(t, manager, "/")
	manager.now = func() time.Time {
		return testQuickTunnelAuthNow.Add(QuickTunnelAuthStateTTL)
	}
	request := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, "broker.assertion")

	callback, err := manager.ConsumeCallback(request)
	assert.Nil(t, callback)
	require.ErrorIs(t, err, errQuickTunnelAuthStateExpired)
}

func TestConsumeQuickTunnelAuthCallbackRejectsBrowserMismatch(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	login := beginTestQuickTunnelLogin(t, manager, "/")
	request := newTestQuickTunnelAuthCallbackRequest(nil, login.State, "broker.assertion")

	callback, err := manager.ConsumeCallback(request)
	assert.Nil(t, callback)
	require.ErrorContains(t, err, "verify authentication-state cookie")
	require.ErrorContains(t, err, "request contains 0 authentication-state cookies, expected one")
}

func TestConsumeQuickTunnelAuthCallbackRejectsTamperedCookie(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	login := beginTestQuickTunnelLogin(t, manager, "/")
	tamperedCookie := &http.Cookie{
		Name:     login.Cookie.Name,
		Value:    login.Cookie.Value + "A",
		Path:     login.Cookie.Path,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	request := newTestQuickTunnelAuthCallbackRequest(tamperedCookie, login.State, "broker.assertion")

	callback, err := manager.ConsumeCallback(request)
	assert.Nil(t, callback)
	require.ErrorContains(t, err, "verify authentication-state cookie")
}

func TestVerifyQuickTunnelAuthStateCookieRejectsInvalidCookie(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		buildCookie func(t *testing.T, manager *QuickTunnelAuthStateManager, login *QuickTunnelAuthLogin) *http.Cookie
		duplicate   bool
	}{
		{
			name: "duplicate cookie",
			buildCookie: func(t *testing.T, _ *QuickTunnelAuthStateManager, login *QuickTunnelAuthLogin) *http.Cookie {
				t.Helper()
				return login.Cookie
			},
			duplicate: true,
		},
		{
			name: "non-canonical state",
			buildCookie: func(t *testing.T, manager *QuickTunnelAuthStateManager, login *QuickTunnelAuthLogin) *http.Cookie {
				t.Helper()
				return newTestQuickTunnelAuthStateCookie(t, manager, quickTunnelAuthStateCookiePayload{
					State:     login.State + "=",
					ExpiresAt: login.Cookie.Expires.Unix(),
				})
			},
		},
		{
			name: "non-positive expiry",
			buildCookie: func(t *testing.T, manager *QuickTunnelAuthStateManager, login *QuickTunnelAuthLogin) *http.Cookie {
				t.Helper()
				return newTestQuickTunnelAuthStateCookie(t, manager, quickTunnelAuthStateCookiePayload{
					State:     login.State,
					ExpiresAt: 0,
				})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestQuickTunnelAuthStateManager(t)
			login := beginTestQuickTunnelLogin(t, manager, "/")
			cookie := test.buildCookie(t, manager, login)
			request := newTestQuickTunnelAuthCallbackRequest(cookie, login.State, "broker.assertion")
			if test.duplicate {
				request.AddCookie(cookie)
			}

			callback, err := manager.ConsumeCallback(request)
			assert.Nil(t, callback)
			require.ErrorContains(t, err, "verify authentication-state cookie")
			if test.duplicate {
				require.ErrorContains(t, err, "request contains 2 authentication-state cookies, expected one")
			}
		})
	}
}

func TestConsumeQuickTunnelAuthCallbackRejectsMismatchedState(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	login := beginTestQuickTunnelLogin(t, manager, "/")
	otherState := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, quickTunnelAuthStateSize))
	request := newTestQuickTunnelAuthCallbackRequest(login.Cookie, otherState, "broker.assertion")

	callback, err := manager.ConsumeCallback(request)
	assert.Nil(t, callback)
	require.EqualError(t, err, "browser state does not match callback state")
}

func TestConsumeQuickTunnelAuthCallbackRejectsInvalidRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{
			name: "wrong method",
			mutate: func(r *http.Request) {
				r.Method = http.MethodGet
			},
		},
		{
			name: "wrong hostname",
			mutate: func(r *http.Request) {
				r.Host = "other.trycloudflare.com"
			},
		},
		{
			name: "unexpected port",
			mutate: func(r *http.Request) {
				r.Host = "test-tunnel.trycloudflare.com:8443"
			},
		},
		{
			name: "wrong path",
			mutate: func(r *http.Request) {
				r.URL.Path = "/callback"
			},
		},
		{
			name: "callback query",
			mutate: func(r *http.Request) {
				r.URL.RawQuery = "state=value"
			},
		},
		{
			name: "callback fragment",
			mutate: func(r *http.Request) {
				r.URL.Fragment = "section"
			},
		},
		{
			name: "wrong content type",
			mutate: func(r *http.Request) {
				r.Header.Set("Content-Type", "application/json")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestQuickTunnelAuthStateManager(t)
			login := beginTestQuickTunnelLogin(t, manager, "/")
			request := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, "broker.assertion")
			test.mutate(request)

			callback, err := manager.ConsumeCallback(request)
			assert.Nil(t, callback)
			require.ErrorContains(t, err, "validate callback request")
		})
	}
}

func TestConsumeQuickTunnelAuthCallbackRejectsInvalidForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "missing state", body: "assertion=broker.assertion"},
		{name: "missing assertion", body: "state=value"},
		{name: "empty state", body: "state=&assertion=broker.assertion"},
		{name: "empty assertion", body: "state=value&assertion="},
		{name: "duplicate state", body: "state=one&state=two&assertion=broker.assertion"},
		{name: "extra field", body: "state=value&assertion=broker.assertion&extra=value"},
		{name: "malformed encoding", body: "state=%zz&assertion=broker.assertion"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestQuickTunnelAuthStateManager(t)
			login := beginTestQuickTunnelLogin(t, manager, "/")
			request := newTestQuickTunnelAuthCallbackRequestWithBody(login.Cookie, test.body)

			callback, err := manager.ConsumeCallback(request)
			assert.Nil(t, callback)
			require.ErrorContains(t, err, "parse callback form")
		})
	}
}

func TestConsumeQuickTunnelAuthCallbackRejectsOversizedForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		contentLength int64
	}{
		{name: "known content length", contentLength: quickTunnelAuthCallbackMaxBodyBytes + 1},
		{name: "unknown content length", contentLength: -1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestQuickTunnelAuthStateManager(t)
			login := beginTestQuickTunnelLogin(t, manager, "/")
			request := newTestQuickTunnelAuthCallbackRequestWithBody(
				login.Cookie,
				strings.Repeat("a", quickTunnelAuthCallbackMaxBodyBytes+1),
			)
			request.ContentLength = test.contentLength

			callback, err := manager.ConsumeCallback(request)
			assert.Nil(t, callback)
			require.ErrorContains(t, err, "parse callback form")
		})
	}
}

func beginTestQuickTunnelLogin(
	t *testing.T,
	manager *QuickTunnelAuthStateManager,
	path string,
) *QuickTunnelAuthLogin {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "https://test-tunnel.trycloudflare.com"+path, nil)
	login, err := manager.BeginLogin(request)
	require.NoError(t, err)
	require.NotNil(t, login)
	return login
}

func newTestQuickTunnelAuthCallbackRequest(
	cookie *http.Cookie,
	state string,
	assertion string,
) *http.Request {
	return newTestQuickTunnelAuthCallbackRequestWithBody(cookie, url.Values{
		"assertion": []string{assertion},
		"state":     []string{state},
	}.Encode())
}

func newTestQuickTunnelAuthCallbackRequestWithBody(cookie *http.Cookie, body string) *http.Request {
	request := httptest.NewRequest(
		http.MethodPost,
		"https://test-tunnel.trycloudflare.com"+QuickTunnelAuthCallbackPath,
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", quickTunnelAuthCallbackContentType+"; charset=UTF-8")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	return request
}

func newTestQuickTunnelAuthStateCookie(
	t *testing.T,
	manager *QuickTunnelAuthStateManager,
	payload quickTunnelAuthStateCookiePayload,
) *http.Cookie {
	t.Helper()

	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	mac := hmac.New(sha256.New, manager.signingKey)
	_, err = mac.Write([]byte(encodedPayload))
	require.NoError(t, err)

	// #nosec G124 -- valid signed test cookies must model the cross-site callback cookie.
	return &http.Cookie{
		Name:     quickTunnelAuthStateCookieName,
		Value:    encodedPayload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)),
		Path:     QuickTunnelAuthCallbackPath,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteNoneMode,
	}
}
