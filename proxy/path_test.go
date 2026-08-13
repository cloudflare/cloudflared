package proxy

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalizeRequestPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rawURL   string
		wantPath string
	}{
		{name: "no dot-segments untouched", rawURL: "http://h/public/admin", wantPath: "/public/admin"},
		{name: "literal dot-segment collapsed", rawURL: "http://h/public/../admin", wantPath: "/admin"},
		{name: "encoded dot-segment collapsed", rawURL: "http://h/public/%2e%2e/admin", wantPath: "/admin"},
		{name: "encoded slash traversal collapsed", rawURL: "http://h/public/..%2fadmin", wantPath: "/admin"},
		{name: "duplicate slashes collapsed", rawURL: "http://h/public//admin", wantPath: "/public/admin"},
		{name: "current-dir segment collapsed", rawURL: "http://h/public/./admin", wantPath: "/public/admin"},
		{name: "trailing slash preserved", rawURL: "http://h/public/../admin/", wantPath: "/admin/"},
		{name: "root preserved", rawURL: "http://h/", wantPath: "/"},
		// RFC 3986 remove_dot_segments absorbs excess ".." on absolute paths
		// rather than escaping root, so these resolve safely to an in-root path.
		{name: "leading dot-segment absorbed to root", rawURL: "http://h/../etc/passwd", wantPath: "/etc/passwd"},
		{name: "excess dot-segments absorbed to root", rawURL: "http://h/public/../../etc/passwd", wantPath: "/etc/passwd"},
		{name: "transient-negative depth stays in root", rawURL: "http://h/a/../../b", wantPath: "/b"},
		// Encoded but dot-free path: rule matching uses the decoded Path, so
		// RawPath must be cleared to keep origin forwarding in agreement.
		{name: "encoded dot-free path re-derived", rawURL: "http://h/admin%2Fsettings", wantPath: "/admin/settings"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(http.MethodGet, tt.rawURL, nil)
			require.NoError(t, err)

			canonicalizeRequestPath(req)

			assert.Equal(t, tt.wantPath, req.URL.Path)
			// RawPath must never survive canonicalization; the forwarded
			// RequestURI is re-derived from the canonical Path.
			assert.Empty(t, req.URL.RawPath)
			assert.Equal(t, tt.wantPath, req.URL.EscapedPath())
		})
	}
}
