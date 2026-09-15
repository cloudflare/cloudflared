package quicktunnelauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	quickTunnelAuthCallbackContentType    = "application/x-www-form-urlencoded"
	quickTunnelAuthCallbackMaxBodyBytes   = 4 * 1024
	quickTunnelAuthCallbackFormFieldCount = 2
)

var errQuickTunnelAuthStateExpired = errors.New("authentication state expired")

// QuickTunnelAuthCallback contains the state consumed from a broker callback.
type QuickTunnelAuthCallback struct {
	State       string
	Assertion   string
	ReturnPath  string
	ClearCookie *http.Cookie
}

// ConsumeCallback validates the browser state attached to a broker callback
// and returns the authenticated result.
//
// The design is stateless: authentication state is signed into the browser
// cookie rather than tracked server-side, so a callback is validated by
// verifying the cookie rather than by looking up a pending state. This means
// a callback whose cookie has not yet expired can be replayed; the broker
// assertion itself is expected to guard against replay across requests.
func (m *QuickTunnelAuthStateManager) ConsumeCallback(r *http.Request) (*QuickTunnelAuthCallback, error) {
	if err := m.validateCallbackRequest(r); err != nil {
		return nil, fmt.Errorf("validate callback request: %w", err)
	}

	state, assertion, err := parseQuickTunnelAuthCallbackForm(r)
	if err != nil {
		return nil, fmt.Errorf("parse callback form: %w", err)
	}

	cookiePayload, err := m.verifyStateCookie(r)
	if err != nil {
		return nil, fmt.Errorf("verify authentication-state cookie: %w", err)
	}
	if !isCanonicalQuickTunnelAuthState(state) ||
		!constantTimeStateEqual(state, cookiePayload.State) {
		return nil, errors.New("browser state does not match callback state")
	}

	now := m.now().UTC()
	cookieExpiresAt := time.Unix(cookiePayload.ExpiresAt, 0).UTC()
	if !now.Before(cookieExpiresAt) {
		return nil, errQuickTunnelAuthStateExpired
	}

	return &QuickTunnelAuthCallback{
		State:       state,
		Assertion:   assertion,
		ReturnPath:  cookiePayload.ReturnPath,
		ClearCookie: newQuickTunnelAuthClearStateCookie(),
	}, nil
}

// validateCallbackRequest verifies the callback method, hostname, URL, and content type.
func (m *QuickTunnelAuthStateManager) validateCallbackRequest(r *http.Request) error {
	if r == nil {
		return errors.New("request cannot be nil")
	}
	if r.URL == nil {
		return errors.New("request URL cannot be nil")
	}
	if r.Method != http.MethodPost {
		return errors.New("callback method must be POST")
	}
	if !matchesQuickTunnelRequestHostname(r.Host, m.hostname) {
		return errors.New("callback hostname does not match protected Quick Tunnel")
	}
	if r.URL.EscapedPath() != QuickTunnelAuthCallbackPath ||
		r.URL.RawQuery != "" ||
		r.URL.Fragment != "" {
		return errors.New("unexpected callback URL")
	}

	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return fmt.Errorf("parse callback content type: %w", err)
	}
	if contentType != quickTunnelAuthCallbackContentType {
		return fmt.Errorf(
			"callback content type %q must be %q",
			contentType,
			quickTunnelAuthCallbackContentType,
		)
	}

	return nil
}

func parseQuickTunnelAuthCallbackForm(r *http.Request) (state, assertion string, err error) {
	if r.Body == nil {
		return "", "", errors.New("callback body is missing")
	}
	if r.ContentLength > quickTunnelAuthCallbackMaxBodyBytes {
		return "", "", fmt.Errorf(
			"callback body is too large: got %d bytes, maximum is %d",
			r.ContentLength,
			quickTunnelAuthCallbackMaxBodyBytes,
		)
	}

	// Read one byte beyond the limit so an unknown-length body that exceeds the
	// maximum can be distinguished from a body exactly at the maximum.
	body, err := io.ReadAll(io.LimitReader(r.Body, quickTunnelAuthCallbackMaxBodyBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read callback body: %w", err)
	}
	// ContentLength may be unknown, so enforce the limit using the bytes actually
	// read as well as the declared length checked above.
	if len(body) > quickTunnelAuthCallbackMaxBodyBytes {
		return "", "", errors.New("callback body is too large")
	}

	form, err := url.ParseQuery(string(body))
	if err != nil {
		return "", "", fmt.Errorf("parse URL-encoded callback body: %w", err)
	}
	if len(form) != quickTunnelAuthCallbackFormFieldCount {
		return "", "", fmt.Errorf(
			"malformed callback form: got %d fields, expected %d",
			len(form),
			quickTunnelAuthCallbackFormFieldCount,
		)
	}

	states := form["state"]
	if len(states) != 1 {
		return "", "", fmt.Errorf("callback state has %d values, expected one", len(states))
	}
	if states[0] == "" {
		return "", "", errors.New("callback state must not be empty")
	}

	assertions := form["assertion"]
	if len(assertions) != 1 {
		return "", "", fmt.Errorf("callback assertion has %d values, expected one", len(assertions))
	}
	if assertions[0] == "" {
		return "", "", errors.New("callback assertion must not be empty")
	}

	return states[0], assertions[0], nil
}

func (m *QuickTunnelAuthStateManager) verifyStateCookie(r *http.Request) (*quickTunnelAuthStateCookiePayload, error) {
	stateCookies := r.CookiesNamed(quickTunnelAuthStateCookieName)
	if len(stateCookies) != 1 {
		return nil, fmt.Errorf(
			"request contains %d authentication-state cookies, expected one",
			len(stateCookies),
		)
	}
	stateCookie := stateCookies[0]

	encodedPayload, encodedSignature, ok := strings.Cut(stateCookie.Value, ".")
	if !ok || encodedPayload == "" || encodedSignature == "" || strings.Contains(encodedSignature, ".") {
		return nil, errors.New("malformed authentication-state cookie")
	}

	signature, err := base64.RawURLEncoding.Strict().DecodeString(encodedSignature)
	if err != nil {
		return nil, fmt.Errorf("decode authentication-state cookie signature: %w", err)
	}
	if len(signature) != sha256.Size {
		return nil, errors.New("malformed authentication-state cookie signature")
	}

	mac := hmac.New(sha256.New, m.signingKey)
	if _, err := mac.Write([]byte(encodedPayload)); err != nil {
		return nil, fmt.Errorf("verify authentication-state cookie signature: %w", err)
	}
	if !hmac.Equal(mac.Sum(nil), signature) {
		return nil, errors.New("invalid authentication-state cookie signature")
	}

	payloadBytes, err := base64.RawURLEncoding.Strict().DecodeString(encodedPayload)
	if err != nil {
		return nil, fmt.Errorf("decode authentication-state cookie payload: %w", err)
	}

	var payload quickTunnelAuthStateCookiePayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("decode authentication-state cookie payload: %w", err)
	}

	if !isCanonicalQuickTunnelAuthState(payload.State) ||
		payload.ExpiresAt <= 0 {
		return nil, errors.New("unsupported authentication-state cookie payload")
	}

	return &payload, nil
}

// constantTimeStateEqual compares sensitive authentication state values in constant time.
func constantTimeStateEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

// isCanonicalQuickTunnelAuthState reports whether state is an unpadded,
// canonical base64url value that decodes to the required state size.
func isCanonicalQuickTunnelAuthState(state string) bool {
	decodedState, err := base64.RawURLEncoding.Strict().DecodeString(state)
	return err == nil && len(decodedState) == quickTunnelAuthStateSize
}

func newQuickTunnelAuthClearStateCookie() *http.Cookie {
	return &http.Cookie{
		Name:     quickTunnelAuthStateCookieName,
		Value:    "",
		Path:     QuickTunnelAuthCallbackPath,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}
