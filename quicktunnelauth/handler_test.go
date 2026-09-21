package quicktunnelauth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQuickTunnelAuthHandlerRejectsNilStateManager(t *testing.T) {
	t.Parallel()

	handler, err := NewQuickTunnelAuthHandler(nil)
	assert.Nil(t, handler)
	require.ErrorContains(t, err, "state manager cannot be nil")
}

func TestQuickTunnelAuthHandlerRejectsInvalidRequest(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	handler, err := NewQuickTunnelAuthHandler(manager)
	require.NoError(t, err)
	response := httptest.NewRecorder()

	err = handler.HandleHTTP(response, nil)
	require.ErrorContains(t, err, "invalid protected Quick Tunnel request")
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, http.StatusText(http.StatusBadRequest)+"\n", response.Body.String())
}

func TestQuickTunnelAuthHandlerRedirectsLoginBeforeOrigin(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	handler, err := NewQuickTunnelAuthHandler(manager)
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodGet,
		"https://test-tunnel.trycloudflare.com/dashboard",
		nil,
	)
	response := httptest.NewRecorder()

	err = handler.HandleHTTP(response, request)
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, response.Code)
	assert.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
	assert.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))

	location, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https", location.Scheme)
	assert.Equal(t, "login.trycloudflare.com", location.Host)
	assert.Equal(t, "/authorize", location.Path)
	assert.Equal(t, "test-tunnel.trycloudflare.com", location.Query().Get("hostname"))
	assert.NotEmpty(t, location.Query().Get("state"))
	assert.Len(t, response.Result().Cookies(), 1)
}

func TestQuickTunnelAuthHandlerRejectsUnsafeMethodBeforeOrigin(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	handler, err := NewQuickTunnelAuthHandler(manager)
	require.NoError(t, err)
	request := httptest.NewRequest(
		http.MethodPost,
		"https://test-tunnel.trycloudflare.com/dashboard",
		nil,
	)
	response := httptest.NewRecorder()

	err = handler.HandleHTTP(response, request)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Equal(t, http.StatusText(http.StatusUnauthorized)+"\n", response.Body.String())
}

func TestQuickTunnelAuthHandlerHandlesInvalidCallbackLocally(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	handler, err := NewQuickTunnelAuthHandler(manager)
	require.NoError(t, err)
	request := newTestQuickTunnelAuthCallbackRequest(nil, "missing-state", "broker.assertion")
	response := httptest.NewRecorder()

	err = handler.HandleHTTP(response, request)
	require.ErrorContains(t, err, "verify authentication-state cookie")
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, http.StatusText(http.StatusBadRequest)+"\n", response.Body.String())
}

func TestQuickTunnelAuthHandlerFailsClosedWithoutValidation(t *testing.T) {
	t.Parallel()

	manager := newTestQuickTunnelAuthStateManager(t)
	handler, err := NewQuickTunnelAuthHandler(manager)
	require.NoError(t, err)
	login := beginTestQuickTunnelLogin(t, manager, "/dashboard")
	request := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, "broker.assertion")
	response := httptest.NewRecorder()

	err = handler.HandleHTTP(response, request)
	require.ErrorIs(t, err, errQuickTunnelAuthCallbackValidationUnavailable)
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Equal(t, http.StatusText(http.StatusForbidden)+"\n", response.Body.String())
	assert.Len(t, response.Result().Cookies(), 1)
	assert.Equal(t, -1, response.Result().Cookies()[0].MaxAge)
}

func TestQuickTunnelAuthHandlerCompletesAuthorizedCallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		allowedMail []string
		email       string
	}{
		{name: "exact email", allowedMail: []string{"visitor@example.com"}, email: "visitor@example.com"},
		{name: "wildcard domain", allowedMail: []string{"*@example.com"}, email: "visitor@example.com"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			harness := newTestQuickTunnelAuthHandlerHarness(t, test.allowedMail)
			request, _ := harness.newCallbackRequest(t, "/dashboard?tab=logs", test.email)
			response := httptest.NewRecorder()

			err := harness.handler.HandleHTTP(response, request)
			require.NoError(t, err)
			assert.Equal(t, http.StatusSeeOther, response.Code)
			assert.Equal(t, "/dashboard?tab=logs", response.Header().Get("Location"))
			assert.Empty(t, response.Body.String())
			assert.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
			assert.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))

			cookies := response.Result().Cookies()
			require.Len(t, cookies, 2)
			assert.Equal(t, quickTunnelAuthStateCookieName, cookies[0].Name)
			assert.Equal(t, -1, cookies[0].MaxAge)
			assert.Equal(t, quickTunnelAuthSessionCookieName, cookies[1].Name)

			valid, err := harness.sessionManager.ValidateSession(
				requestWithQuickTunnelAuthSession(cookies[1].Value),
			)
			require.NoError(t, err)
			assert.True(t, valid)
		})
	}
}

func TestQuickTunnelAuthHandlerRejectsUnauthorizedRecipient(t *testing.T) {
	t.Parallel()

	harness := newTestQuickTunnelAuthHandlerHarness(t, []string{"allowed@example.com"})
	request, _ := harness.newCallbackRequest(t, "/dashboard", "visitor@example.com")
	response := httptest.NewRecorder()

	err := harness.handler.HandleHTTP(response, request)
	require.ErrorIs(t, err, errQuickTunnelAuthRecipientNotAllowed)
	assert.NotContains(t, err.Error(), "visitor@example.com")
	assert.NotContains(t, err.Error(), "allowed@example.com")
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Equal(t, http.StatusText(http.StatusForbidden)+"\n", response.Body.String())

	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, quickTunnelAuthStateCookieName, cookies[0].Name)
	assert.Equal(t, -1, cookies[0].MaxAge)
}

func TestQuickTunnelAuthHandlerRejectsInvalidBrokerAssertion(t *testing.T) {
	t.Parallel()

	harness := newTestQuickTunnelAuthHandlerHarness(t, []string{"visitor@example.com"})
	login := beginTestQuickTunnelLogin(t, harness.stateManager, "/dashboard")
	request := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, "not-a-jwt")
	response := httptest.NewRecorder()

	err := harness.handler.HandleHTTP(response, request)
	require.ErrorContains(t, err, "parse broker assertion")
	assert.NotContains(t, err.Error(), "not-a-jwt")
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Equal(t, http.StatusText(http.StatusForbidden)+"\n", response.Body.String())
	assert.Len(t, response.Result().Cookies(), 1)

	// A raw client can retain and retry the callback artifacts even though a
	// normal browser applies the state-cookie deletion response.
	retryResponse := httptest.NewRecorder()
	retryRequest := newTestQuickTunnelAuthCallbackRequest(
		login.Cookie,
		login.State,
		harness.signAssertion(t, login.State, "visitor@example.com"),
	)
	require.NoError(t, harness.handler.HandleHTTP(retryResponse, retryRequest))
	assert.Equal(t, http.StatusSeeOther, retryResponse.Code)
}

// TestQuickTunnelAuthHandlerAllowsCallbackReplayWithinAssertionTTL documents
// the intentional stateless trade-off. Normal browsers delete the state cookie
// from the first response; a raw client retaining all callback artifacts can
// replay them until the broker assertion expires.
func TestQuickTunnelAuthHandlerAllowsCallbackReplayWithinAssertionTTL(t *testing.T) {
	t.Parallel()

	harness := newTestQuickTunnelAuthHandlerHarness(t, []string{"visitor@example.com"})
	login := beginTestQuickTunnelLogin(t, harness.stateManager, "/dashboard")
	assertion := harness.signAssertion(t, login.State, "visitor@example.com")

	firstResponse := httptest.NewRecorder()
	firstRequest := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, assertion)
	require.NoError(t, harness.handler.HandleHTTP(firstResponse, firstRequest))
	assert.Equal(t, http.StatusSeeOther, firstResponse.Code)

	secondResponse := httptest.NewRecorder()
	secondRequest := newTestQuickTunnelAuthCallbackRequest(login.Cookie, login.State, assertion)
	require.NoError(t, harness.handler.HandleHTTP(secondResponse, secondRequest))
	assert.Equal(t, http.StatusSeeOther, secondResponse.Code)
	cookies := secondResponse.Result().Cookies()
	require.Len(t, cookies, 2)
	assert.Equal(t, quickTunnelAuthSessionCookieName, cookies[1].Name)
}
