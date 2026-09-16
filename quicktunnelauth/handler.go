package quicktunnelauth

import (
	"errors"
	"net/http"
)

var errQuickTunnelAuthCallbackValidationUnavailable = errors.New("broker assertion validation is not configured")

// QuickTunnelAuthHandler intercepts protected Quick Tunnel requests before
// ingress selection.
type QuickTunnelAuthHandler struct {
	stateManager *QuickTunnelAuthStateManager
}

// NewQuickTunnelAuthHandler creates a pre-origin handler backed by the provided
// browser authentication state manager.
func NewQuickTunnelAuthHandler(stateManager *QuickTunnelAuthStateManager) (*QuickTunnelAuthHandler, error) {
	if stateManager == nil {
		return nil, errors.New("authentication state manager cannot be nil")
	}
	return &QuickTunnelAuthHandler{stateManager: stateManager}, nil
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
	// TODO(TUN-10801): Validate callback.Assertion against the broker JWKS and
	// bind its state and hostname claims before applying recipient rules.
	// TODO(TUN-10802): Issue the local session and redirect with HTTP 303 to
	// callback.ReturnPath. Until then, fail closed after consuming the state.
	http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
	return errQuickTunnelAuthCallbackValidationUnavailable
}
