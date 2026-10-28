package quicktunnelauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var errQuickTunnelAuthRecipientNotAllowed = errors.New("broker assertion identity is not authorized")

// authorizeCallback validates a broker callback, enforces the configured
// recipient rules, and creates the process-local authentication session.
func (h *QuickTunnelAuthHandler) authorizeCallback(
	ctx context.Context,
	callback *QuickTunnelAuthCallback,
) (*http.Cookie, error) {
	if h == nil || h.stateManager == nil || h.assertionValidator == nil ||
		h.sessionManager == nil || h.recipientPolicy == nil {
		return nil, errQuickTunnelAuthCallbackValidationUnavailable
	}
	if ctx == nil {
		return nil, errors.New("authorize authentication callback: context is nil")
	}
	if callback == nil {
		return nil, errors.New("authorize authentication callback: callback is nil")
	}

	identity, err := h.assertionValidator.Validate(
		ctx,
		callback.Assertion,
		h.stateManager.hostname,
		callback.State,
	)
	if err != nil {
		return nil, fmt.Errorf("validate broker assertion: %w", err)
	}
	if identity == nil {
		return nil, errors.New("validate broker assertion: identity is nil")
	}

	if !h.recipientPolicy.allows(identity.Email) {
		return nil, errQuickTunnelAuthRecipientNotAllowed
	}

	sessionCookie, err := h.sessionManager.IssueSession(identity.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("issue authentication session: %w", err)
	}
	if sessionCookie == nil {
		return nil, errors.New("issue authentication session: cookie is nil")
	}
	return sessionCookie, nil
}
