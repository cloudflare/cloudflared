package quicktunnelauth

import (
	"errors"
	"net/http"
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

// HandleHTTP redirects new browser logins and handles the reserved broker
// callback without allowing either request to reach the origin.
func (h *QuickTunnelAuthHandler) HandleHTTP(w http.ResponseWriter, r *http.Request) error {
	// private: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cache-Control#private
	// no-store: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cache-Control#no-store
	w.Header().Set("Cache-Control", "private, no-store")
	// no-referrer: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Referrer-Policy#no-referrer_2
	w.Header().Set("Referrer-Policy", "no-referrer")

	if r == nil || r.URL == nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return errors.New("invalid protected Quick Tunnel request")
	}

	if r.URL.EscapedPath() == QuickTunnelAuthCallbackPath {
		return h.handleCallback(w, r)
	}

	// TODO(TUN-10803): Validate the process-local session cookie and allow
	// authenticated requests to reach the origin. Valid sessions must have the
	// internal cookie removed before proxying.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return nil
	}

	login, err := h.stateManager.BeginLogin(r)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return err
	}

	http.SetCookie(w, login.Cookie)
	w.Header().Set("Location", login.RedirectURL.String())
	w.WriteHeader(http.StatusFound)
	return nil
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
