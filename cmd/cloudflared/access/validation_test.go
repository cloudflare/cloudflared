package access

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRequestHeaders(t *testing.T) {
	values := parseRequestHeaders([]string{"client: value", "secret: safe-value", "trash", "cf-trace-id: 000:000:0:1:asd"})
	assert.Len(t, values, 3)
	assert.Equal(t, "value", values.Get("client"))
	assert.Equal(t, "safe-value", values.Get("secret"))
	assert.Equal(t, "000:000:0:1:asd", values.Get("cf-trace-id"))
}

func TestParseURL(t *testing.T) {
	schemes := []string{
		"http://",
		"https://",
		"",
	}
	hosts := []struct {
		input    string
		expected string
	}{
		{"localhost", "localhost"},
		{"127.0.0.1", "127.0.0.1"},
		{"127.0.0.1:9090", "127.0.0.1:9090"},
		{"::1", "::1"},
		{"::1:8080", "::1:8080"},
		{"[::1]", "[::1]"},
		{"[::1]:8080", "[::1]:8080"},
		{":8080", ":8080"},
		{"example.com", "example.com"},
		{"hello.example.com", "hello.example.com"},
		{"bücher.example.com", "xn--bcher-kva.example.com"},
	}
	paths := []string{
		"",
		"/test",
		"/example.com?qwe=123",
	}
	for i, scheme := range schemes {
		for j, host := range hosts {
			for k, path := range paths {
				t.Run(fmt.Sprintf("%d_%d_%d", i, j, k), func(t *testing.T) {
					input := fmt.Sprintf("%s%s%s", scheme, host.input, path)
					expected := fmt.Sprintf("%s%s%s", "https://", host.expected, path)
					url, err := parseURL(input)
					assert.NoError(t, err, "input: %s\texpected: %s", input, expected)
					assert.Equal(t, expected, url.String())
					assert.Equal(t, host.expected, url.Host)
					assert.Equal(t, "https", url.Scheme)
				})
			}
		}
	}

	t.Run("no input", func(t *testing.T) {
		_, err := parseURL("")
		assert.ErrorContains(t, err, "no input provided")
	})

	t.Run("missing host", func(t *testing.T) {
		_, err := parseURL("https:///host")
		assert.ErrorContains(t, err, "failed to parse Host")
	})

	t.Run("invalid path only", func(t *testing.T) {
		_, err := parseURL("/host")
		assert.ErrorContains(t, err, "failed to parse Host")
	})

	t.Run("invalid parse URL", func(t *testing.T) {
		_, err := parseURL("https://host\\host")
		assert.ErrorContains(t, err, "failed to parse as URL")
	})
}
