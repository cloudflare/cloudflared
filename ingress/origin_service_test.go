package ingress

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddPortIfMissing(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"ssh://[::1]", "[::1]:22"},
		{"ssh://[::1]:38", "[::1]:38"},
		{"ssh://abc:38", "abc:38"},
		{"ssh://127.0.0.1:38", "127.0.0.1:38"},
		{"ssh://127.0.0.1", "127.0.0.1:22"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			url1, _ := url.Parse(tc.input)
			addPortIfMissing(url1, 22)
			require.Equal(t, tc.expected, url1.Host)
		})
	}
}
