package quicktunnelauth

import (
	"net/http"
	"strings"
)

const (
	quickTunnelAuthCacheControlValue   = "private, no-store"
	quickTunnelAuthReferrerPolicyValue = "no-referrer"
)

// FilterQuickTunnelsAuthHeaders enforces protected-response isolation at every
// response-writing boundary. It prevents an origin from overriding the private
// cache and referrer policies or setting cloudflared's process-local
// authentication cookies, while preserving unrelated origin cookies.
func FilterQuickTunnelsAuthHeaders(headers http.Header) {
	if headers == nil {
		return
	}

	setCookies := headers.Values("Set-Cookie")
	headers.Del("Set-Cookie")
	for _, value := range setCookies {
		if !isQuickTunnelAuthSetCookie(value) {
			headers.Add("Set-Cookie", value)
		}
	}

	headers.Set("Cache-Control", quickTunnelAuthCacheControlValue)
	headers.Set("Referrer-Policy", quickTunnelAuthReferrerPolicyValue)
}

// removeQuickTunnelAuthCookies removes process-local authentication cookies
// before a request is proxied while preserving unrelated origin cookies.
func removeQuickTunnelAuthCookies(request *http.Request) {
	if request == nil {
		return
	}

	cookies := request.Cookies()
	request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if !isQuickTunnelAuthCookieName(cookie.Name) {
			request.AddCookie(cookie)
		}
	}
}

func isQuickTunnelAuthSetCookie(value string) bool {
	cookiePair, _, _ := strings.Cut(value, ";")
	name, _, _ := strings.Cut(cookiePair, "=")
	return isQuickTunnelAuthCookieName(strings.TrimSpace(name))
}

func isQuickTunnelAuthCookieName(name string) bool {
	return name == quickTunnelAuthStateCookieName || name == quickTunnelAuthSessionCookieName
}
