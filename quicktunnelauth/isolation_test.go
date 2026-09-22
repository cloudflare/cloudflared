package quicktunnelauth

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterQuickTunnelsAuthHeaders(t *testing.T) {
	t.Parallel()

	headers := http.Header{
		"Cache-Control":   {"public, max-age=3600"},
		"Referrer-Policy": {"unsafe-url"},
		"Set-Cookie": {
			quickTunnelAuthSessionCookieName + "=session-value; Path=/; Secure; HttpOnly; SameSite=Lax",
			quickTunnelAuthStateCookieName + "=state-value; Path=/; Secure; HttpOnly; SameSite=Lax",
			quickTunnelAuthSessionCookieName + "; Path=/; Secure; Max-Age=0",
			quickTunnelAuthStateCookieName + "; Path=/; Secure; Max-Age=0",
			"origin-session=origin-value; Path=/; Secure; HttpOnly; SameSite=Lax",
		},
	}

	FilterQuickTunnelsAuthHeaders(headers)

	assert.Equal(t, quickTunnelAuthCacheControlValue, headers.Get("Cache-Control"))
	assert.Equal(t, quickTunnelAuthReferrerPolicyValue, headers.Get("Referrer-Policy"))
	assert.Equal(t, []string{
		"origin-session=origin-value; Path=/; Secure; HttpOnly; SameSite=Lax",
	}, headers.Values("Set-Cookie"))
}

func TestFilterQuickTunnelsAuthHeadersHandlesNil(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		FilterQuickTunnelsAuthHeaders(nil)
	})
}
