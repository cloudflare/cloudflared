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
