package quicktunnelauth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/cloudflare/cloudflared/connection"
)

const (
	quickTunnelAuthOutcomeInvalidRequest         = "invalid_request"
	quickTunnelAuthOutcomeCallbackRejected       = "callback_rejected"
	quickTunnelAuthOutcomeCallbackAuthorized     = "callback_authorized"
	quickTunnelAuthOutcomeSessionValidationError = "session_validation_error"
	quickTunnelAuthOutcomeSessionValid           = "session_valid"
	quickTunnelAuthOutcomeUnauthenticatedMethod  = "unauthenticated_unsafe_method"
	quickTunnelAuthOutcomeLoginStartError        = "login_start_error"
	quickTunnelAuthOutcomeLoginRedirect          = "login_redirect"
)

var errQuickTunnelAuthCallbackValidationUnavailable = errors.New("broker assertion validation is not configured")

// QuickTunnelAuthHandler intercepts protected Quick Tunnel requests before
// ingress selection.
type QuickTunnelAuthHandler struct {
	stateManager       *QuickTunnelAuthStateManager
	assertionValidator *QuickTunnelAuthAssertionValidator
	sessionManager     *QuickTunnelAuthSessionManager
	recipientPolicy    *QuickTunnelAuthRecipientPolicy
}

// NewQuickTunnelAuthHandler creates a pre-origin handler backed by the provided
// browser authentication state manager.
func NewQuickTunnelAuthHandler(stateManager *QuickTunnelAuthStateManager) (*QuickTunnelAuthHandler, error) {
	if stateManager == nil {
		return nil, errors.New("authentication state manager cannot be nil")
	}
	return &QuickTunnelAuthHandler{stateManager: stateManager}, nil
}

// NewQuickTunnelAuthHandlerWithAuthorization creates a fully configured
// protected Quick Tunnel authentication handler.
func NewQuickTunnelAuthHandlerWithAuthorization(
	stateManager *QuickTunnelAuthStateManager,
	assertionValidator *QuickTunnelAuthAssertionValidator,
	sessionManager *QuickTunnelAuthSessionManager,
	recipientPolicy *QuickTunnelAuthRecipientPolicy,
) (*QuickTunnelAuthHandler, error) {
	handler, err := NewQuickTunnelAuthHandler(stateManager)
	if err != nil {
		return nil, err
	}
	if assertionValidator == nil {
		return nil, errors.New("authentication assertion validator cannot be nil")
	}
	if sessionManager == nil {
		return nil, errors.New("authentication session manager cannot be nil")
	}
	if recipientPolicy == nil {
		return nil, errors.New("authentication recipient policy cannot be nil")
	}

	handler.assertionValidator = assertionValidator
	handler.sessionManager = sessionManager
	handler.recipientPolicy = recipientPolicy
	return handler, nil
}

// AuthorizeHTTP handles unauthenticated requests locally and allows requests
// with a valid session to continue to the origin. An allowed decision is
// returned only after the session cookie has been removed. Outcome is a fixed
// reason code suitable for structured logging.
func (h *QuickTunnelAuthHandler) AuthorizeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) (decision connection.HTTPRequestAuthorizationDecision, outcome string, err error) {
	// private: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cache-Control#private
	// no-store: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cache-Control#no-store
	w.Header().Set("Cache-Control", quickTunnelAuthCacheControlValue)
	// no-referrer: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Referrer-Policy#no-referrer_2
	w.Header().Set("Referrer-Policy", quickTunnelAuthReferrerPolicyValue)

	if r == nil || r.URL == nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return connection.HTTPRequestAuthorizationHandled, quickTunnelAuthOutcomeInvalidRequest, errors.New("invalid protected Quick Tunnel request")
	}

	if r.URL.EscapedPath() == QuickTunnelAuthCallbackPath {
		if err := h.handleCallback(w, r); err != nil {
			return connection.HTTPRequestAuthorizationHandled, quickTunnelAuthOutcomeCallbackRejected, err
		}
		return connection.HTTPRequestAuthorizationHandled, quickTunnelAuthOutcomeCallbackAuthorized, nil
	}

	if h.sessionManager != nil {
		valid, err := h.sessionManager.ValidateSession(r)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return connection.HTTPRequestAuthorizationHandled, quickTunnelAuthOutcomeSessionValidationError, fmt.Errorf("validate authentication session: %w", err)
		}
		if valid {
			return connection.HTTPRequestAuthorizationAllowed, quickTunnelAuthOutcomeSessionValid, nil
		}
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return connection.HTTPRequestAuthorizationHandled, quickTunnelAuthOutcomeUnauthenticatedMethod, nil
	}

	login, err := h.stateManager.BeginLogin(r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return connection.HTTPRequestAuthorizationHandled, quickTunnelAuthOutcomeLoginStartError, err
	}

	http.SetCookie(w, login.Cookie)
	w.Header().Set("Location", login.RedirectURL.String())
	w.WriteHeader(http.StatusFound)
	return connection.HTTPRequestAuthorizationHandled, quickTunnelAuthOutcomeLoginRedirect, nil
}

func (h *QuickTunnelAuthHandler) handleCallback(w http.ResponseWriter, r *http.Request) error {
	callback, err := h.stateManager.ConsumeCallback(r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return err
	}

	http.SetCookie(w, callback.ClearCookie)
	sessionCookie, err := h.authorizeCallback(r.Context(), callback)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return err
	}

	http.SetCookie(w, sessionCookie)
	w.Header().Set("Location", callback.ReturnPath)
	w.WriteHeader(http.StatusSeeOther)
	return nil
}
