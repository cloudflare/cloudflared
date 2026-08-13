package proxy

import (
	"net/http"
	"path"
	"strings"
)

// canonicalizeRequestPath collapses dot-segments on r.URL so that ingress
// rule selection and origin forwarding operate on the same path.
// r.URL.Path is already percent-decoded by net/url, so encoded variants
// (e.g. %2e%2e, ..%2f) are handled too.
func canonicalizeRequestPath(r *http.Request) {
	if r.URL == nil || r.URL.Path == "" {
		return
	}

	// Collapse dot-segments and duplicate slashes via RFC 3986 remove_dot_segments.
	// path.Clean absorbs any excess ".." on an absolute path, so the result can
	// never escape root.
	cleaned := path.Clean(r.URL.Path)
	// path.Clean strips a trailing slash, but it is meaningful to many origins
	// (e.g. WordPress redirect_canonical), so restore it.
	if cleaned != "/" && strings.HasSuffix(r.URL.Path, "/") {
		cleaned += "/"
	}

	r.URL.Path = cleaned

	// Clear any RawPath so net/url re-derives the escaped path from the
	// canonical Path. Otherwise, an encoded but dot-free RawPath
	// (e.g. /admin%2Fsettings) would be forwarded verbatim while rule
	// selection ran against the decoded Path.
	r.URL.RawPath = ""
}
