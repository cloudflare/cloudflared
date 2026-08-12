package proxy

import (
	"net/http"
	"path"
)

// canonicalizeRequestPath applies RFC 3986 remove_dot_segments to r.URL so that
// ingress rule selection and origin forwarding operate on the same path,
// preventing dot-segment traversal from selecting a permissive rule while the
// origin resolves to a different resource. r.URL.Path is already percent-decoded
// by net/url, so encoded variants (e.g. %2e%2e, ..%2f) are handled too.
func canonicalizeRequestPath(r *http.Request) {
	if r.URL == nil || r.URL.Path == "" {
		return
	}

	r.URL.Path = path.Clean(r.URL.Path)

	// Clear RawPath so net/url re-derives the escaped path from the canonical
	// Path. Otherwise an encoded but dot-free RawPath (e.g. /admin%2Fsettings)
	// would be forwarded verbatim while rule selection ran against the decoded
	// Path, letting the two disagree.
	r.URL.RawPath = ""
}
