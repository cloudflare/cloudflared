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
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	QuickTunnelAuthCallbackPath       = "/.cloudflared/qt-auth/callback"
	QuickTunnelAuthBrokerAuthorizeURL = "https://login.trycloudflare.com/authorize"
	QuickTunnelAuthStateTTL           = 10 * time.Minute

	quickTunnelAuthSigningKeySize      = 32
	quickTunnelAuthStateSize           = 32
	quickTunnelAuthMaxStateCookieBytes = 4 * 1024
	// __Secure- enforces HTTPS and the Secure attribute. __Host- cannot be used
	// because it requires Path=/, while this cookie is scoped to the callback path.
	// See https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Cookies#cookie_prefixes.
	quickTunnelAuthStateCookiePrefix = "__Secure-cloudflared-qt-auth-state-"
	quickTunnelAuthHTTPSPort         = 443

	// quickTunnelAuthMaxLabelLength is the maximum length of a DNS label.
	// Quick Tunnel labels are restricted to ASCII alphanumeric characters
	// and hyphens.
	quickTunnelAuthMaxLabelLength = 63
)

var errInvalidQuickTunnelReturnPath = errors.New("invalid Quick Tunnel authentication return path")

// QuickTunnelAuthStateManager creates browser-bound authentication states.
type QuickTunnelAuthStateManager struct {
	hostname   string
	brokerURL  url.URL
	signingKey []byte
	random     io.Reader
	now        func() time.Time
}

type quickTunnelAuthStateCookiePayload struct {
	State      string `json:"state"`
	Hostname   string `json:"hostname"`
	ReturnPath string `json:"return_path"`
	ExpiresAt  int64  `json:"exp"`
}

// QuickTunnelAuthLogin contains the browser state needed to start one login.
type QuickTunnelAuthLogin struct {
	State       string
	RedirectURL *url.URL
	Cookie      *http.Cookie
}

func NewQuickTunnelAuthStateManager(hostname string) (*QuickTunnelAuthStateManager, error) {
	brokerURL, err := url.Parse(QuickTunnelAuthBrokerAuthorizeURL)
	if err != nil {
		return nil, fmt.Errorf("parse broker URL: %w", err)
	}

	normalizedHostname := strings.ToLower(hostname)
	if !isQuickTunnelHostname(normalizedHostname) {
		return nil, fmt.Errorf("%q is not a valid Quick Tunnel hostname", hostname)
	}

	signingKey := make([]byte, quickTunnelAuthSigningKeySize)
	if _, err := rand.Read(signingKey); err != nil {
		return nil, fmt.Errorf("generate authentication-state signing key: %w", err)
	}

	return &QuickTunnelAuthStateManager{
		hostname:   normalizedHostname,
		brokerURL:  *brokerURL,
		signingKey: signingKey,
		random:     rand.Reader,
		now:        time.Now,
	}, nil
}

// isQuickTunnelHostname reports whether hostname is a valid protected Quick
// Tunnel hostname of the form <label>.trycloudflare.com.
//
// The first label must be a non-empty DNS label using only ASCII letters,
// digits, and hyphens, and it must not start or end with a hyphen.
func isQuickTunnelHostname(hostname string) bool {
	labels := strings.Split(hostname, ".")
	if len(labels) != 3 {
		return false
	}

	if labels[1] != "trycloudflare" || labels[2] != "com" {
		return false
	}

	label := labels[0]
	if label == "" || len(label) > quickTunnelAuthMaxLabelLength {
		return false
	}

	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}

	for _, r := range label {
		if !isQuickTunnelLabelRune(r) {
			return false
		}
	}

	return true
}

// isQuickTunnelLabelRune reports whether r is an ASCII letter, digit, or
// hyphen. DNS labels are restricted to ASCII, so we do not use unicode.IsLetter.
func isQuickTunnelLabelRune(r rune) bool {
	return ('a' <= r && r <= 'z') ||
		('0' <= r && r <= '9') ||
		r == '-'
}

// matchesQuickTunnelRequestHostname reports whether authority matches the
// configured Quick Tunnel hostname. An explicit :443 port is accepted.
func matchesQuickTunnelRequestHostname(authority, expectedHostname string) bool {
	requestHostname := authority
	if hostname, port, err := net.SplitHostPort(authority); err == nil {
		portNumber, err := strconv.ParseUint(port, 10, 16)
		if err != nil || portNumber != quickTunnelAuthHTTPSPort {
			return false
		}
		requestHostname = hostname
	}

	return strings.EqualFold(requestHostname, expectedHostname)
}

// validateQuickTunnelReturnPath validates and canonicalizes a browser return
// path, for example converting "/café" to "/caf%C3%A9".
func validateQuickTunnelReturnPath(rawReturnPath string) (string, error) {
	// ParseRequestURI treats a literal '#' as path data because HTTP request
	// targets do not contain fragments. Reject it instead of silently
	// canonicalizing a fragment-like value to "%23".
	if strings.Contains(rawReturnPath, "#") {
		return "", errInvalidQuickTunnelReturnPath
	}

	returnURL, err := url.ParseRequestURI(rawReturnPath)
	if err != nil {
		return "", errInvalidQuickTunnelReturnPath
	}

	// A safe return target must be an absolute path on the current host. Reject
	// URLs with a scheme, authority, user information, or opaque scheme-specific
	// data so the redirect cannot leave the protected Quick Tunnel origin.
	if returnURL.IsAbs() ||
		!strings.HasPrefix(returnURL.Path, "/") ||
		returnURL.Host != "" ||
		returnURL.User != nil ||
		returnURL.Opaque != "" {
		return "", errInvalidQuickTunnelReturnPath
	}

	// Reject paths that become cross-origin network paths after URL decoding or
	// browser backslash normalization. For example, "/%2F%2Fevil.example"
	// decodes with a "//" prefix, while "/%5Cevil.example" can normalize to
	// "//evil.example".
	// See RFC 3986, Section 4.2:
	// https://datatracker.ietf.org/doc/html/rfc3986#section-4.2.
	if strings.HasPrefix(returnURL.Path, "//") || strings.Contains(returnURL.Path, "\\") {
		return "", errInvalidQuickTunnelReturnPath
	}

	if returnURL.Path == QuickTunnelAuthCallbackPath {
		return "", errInvalidQuickTunnelReturnPath
	}

	return returnURL.RequestURI(), nil
}

// BeginLogin creates a signed browser state and broker redirect for an
// unauthenticated GET or HEAD request.
func (m *QuickTunnelAuthStateManager) BeginLogin(r *http.Request) (*QuickTunnelAuthLogin, error) {
	if r == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}
	if r.URL == nil {
		return nil, fmt.Errorf("request URL cannot be nil")
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return nil, fmt.Errorf(
			"unsupported method %q for authentication redirect; only GET and HEAD are allowed",
			r.Method,
		)
	}

	if !matchesQuickTunnelRequestHostname(r.Host, m.hostname) {
		return nil, fmt.Errorf(
			"request hostname %q does not match protected Quick Tunnel hostname",
			r.Host,
		)
	}

	returnPath, err := validateQuickTunnelReturnPath(r.URL.RequestURI())
	if err != nil {
		return nil, fmt.Errorf("validate authentication return path: %w", err)
	}

	state, err := generateQuickTunnelAuthState(m.random)
	if err != nil {
		return nil, err
	}

	now := m.now().UTC()
	expiresAt := now.Add(QuickTunnelAuthStateTTL)
	cookie, err := m.newStateCookie(state, returnPath, expiresAt)
	if err != nil {
		return nil, err
	}

	redirectURL := m.brokerURL
	redirectURL.RawQuery = url.Values{
		"hostname": []string{m.hostname},
		"state":    []string{state},
	}.Encode()

	return &QuickTunnelAuthLogin{
		State:       state,
		RedirectURL: &redirectURL,
		Cookie:      cookie,
	}, nil
}

// generateQuickTunnelAuthState returns a URL-safe base64-encoded random state.
func generateQuickTunnelAuthState(random io.Reader) (string, error) {
	state := make([]byte, quickTunnelAuthStateSize)
	if _, err := io.ReadFull(random, state); err != nil {
		return "", fmt.Errorf("generate authentication state: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(state), nil
}

func quickTunnelAuthStateCookieName(state string) string {
	return quickTunnelAuthStateCookiePrefix + state
}

// newStateCookie returns the signed, host-only cookie that binds the browser
// to one stateless authentication flow.
func (m *QuickTunnelAuthStateManager) newStateCookie(state, returnPath string, expiresAt time.Time) (*http.Cookie, error) {
	payload, err := json.Marshal(quickTunnelAuthStateCookiePayload{
		State:      state,
		Hostname:   m.hostname,
		ReturnPath: returnPath,
		ExpiresAt:  expiresAt.Unix(),
	})
	if err != nil {
		return nil, fmt.Errorf("encode authentication-state cookie: %w", err)
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, m.signingKey)
	if _, err := mac.Write([]byte(encodedPayload)); err != nil {
		return nil, fmt.Errorf("sign authentication-state cookie: %w", err)
	}

	encodedSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	// The broker callback is a cross-site POST from login.trycloudflare.com
	// to <tunnel>.trycloudflare.com. Because trycloudflare.com is a public
	// suffix, those hosts are treated as separate sites by browsers, so a
	// Lax cookie would not be sent on the callback. SameSite=None is required
	// here, while Secure and HttpOnly remain enabled.
	// #nosec G124 -- cross-site callback requires SameSite=None
	cookie := &http.Cookie{
		Name:     quickTunnelAuthStateCookieName(state),
		Value:    encodedPayload + "." + encodedSignature,
		Path:     QuickTunnelAuthCallbackPath,
		Expires:  expiresAt,
		MaxAge:   int(QuickTunnelAuthStateTTL / time.Second),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteNoneMode,
	}
	if len(cookie.String()) > quickTunnelAuthMaxStateCookieBytes {
		return nil, fmt.Errorf("authentication-state cookie exceeds %d-byte limit", quickTunnelAuthMaxStateCookieBytes)
	}
	return cookie, nil
}
