package validation

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateHostname(t *testing.T) {
	var inputHostname string
	hostname, err := ValidateHostname(inputHostname)
	require.NoError(t, err)
	assert.Empty(t, hostname)

	inputHostname = "hello.example.com"
	hostname, err = ValidateHostname(inputHostname)
	require.NoError(t, err)
	assert.Equal(t, "hello.example.com", hostname)

	inputHostname = "http://hello.example.com"
	hostname, err = ValidateHostname(inputHostname)
	require.NoError(t, err)
	assert.Equal(t, "hello.example.com", hostname)

	inputHostname = "bücher.example.com"
	hostname, err = ValidateHostname(inputHostname)
	require.NoError(t, err)
	assert.Equal(t, "xn--bcher-kva.example.com", hostname)

	inputHostname = "http://bücher.example.com"
	hostname, err = ValidateHostname(inputHostname)
	require.NoError(t, err)
	assert.Equal(t, "xn--bcher-kva.example.com", hostname)

	inputHostname = "http%3A%2F%2Fhello.example.com"
	hostname, err = ValidateHostname(inputHostname)
	require.NoError(t, err)
	assert.Equal(t, "hello.example.com", hostname)
}

func TestValidateUrl(t *testing.T) {
	type testCase struct {
		input          string
		expectedOutput string
	}
	testCases := []testCase{
		{"http://localhost", "http://localhost"},
		{"http://localhost/", "http://localhost"},
		{"http://localhost/api", "http://localhost"},
		{"http://localhost/api/", "http://localhost"},
		{"https://localhost", "https://localhost"},
		{"https://localhost/", "https://localhost"},
		{"https://localhost/api", "https://localhost"},
		{"https://localhost/api/", "https://localhost"},
		{"https://localhost:8080", "https://localhost:8080"},
		{"https://localhost:8080/", "https://localhost:8080"},
		{"https://localhost:8080/api", "https://localhost:8080"},
		{"https://localhost:8080/api/", "https://localhost:8080"},
		{"localhost", "http://localhost"},
		{"localhost/", "http://localhost/"},
		{"localhost/api", "http://localhost/api"},
		{"localhost/api/", "http://localhost/api/"},
		{"localhost:8080", "http://localhost:8080"},
		{"localhost:8080/", "http://localhost:8080/"},
		{"localhost:8080/api", "http://localhost:8080/api"},
		{"localhost:8080/api/", "http://localhost:8080/api/"},
		{"localhost:8080/api/?asdf", "http://localhost:8080/api/?asdf"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"127.0.0.1", "http://127.0.0.1"},
		{"https://127.0.0.1:8080", "https://127.0.0.1:8080"},
		{"[::1]:8080", "http://[::1]:8080"},
		{"http://[::1]", "http://[::1]"},
		{"http://[::1]:8080", "http://[::1]:8080"},
		{"[::1]", "http://[::1]"},
		{"https://example.com", "https://example.com"},
		{"example.com", "http://example.com"},
		{"http://hello.example.com", "http://hello.example.com"},
		{"hello.example.com", "http://hello.example.com"},
		{"hello.example.com:8080", "http://hello.example.com:8080"},
		{"https://hello.example.com:8080", "https://hello.example.com:8080"},
		{"https://bücher.example.com", "https://xn--bcher-kva.example.com"},
		{"bücher.example.com", "http://xn--bcher-kva.example.com"},
		{"https%3A%2F%2Fhello.example.com", "https://hello.example.com"},
		{"https://alex:12345@hello.example.com:8080", "https://hello.example.com:8080"},
	}
	for i, testCase := range testCases {
		validUrl, err := ValidateUrl(testCase.input)
		require.NoError(t, err, "test case %v", i)
		assert.Equal(t, testCase.expectedOutput, validUrl.String(), "test case %v", i)
	}

	validUrl, err := ValidateUrl("")
	assert.Equal(t, fmt.Errorf("URL should not be empty"), err)
	assert.Empty(t, validUrl)

	validUrl, err = ValidateUrl("ftp://alex:12345@hello.example.com:8080/robot.txt")
	assert.Equal(t, "Currently Cloudflare Tunnel does not support ftp protocol.", err.Error())
	assert.Empty(t, validUrl)

	for _, input := range []string{
		"http://::1/",
		"http://localhost:80:80/",
	} {
		t.Run("reject malformed host "+input, func(t *testing.T) {
			validURL, err := ValidateUrl(input)
			require.Error(t, err)
			assert.Nil(t, validURL)
		})
	}
}

func TestNewAccessValidatorOk(t *testing.T) {
	ctx := context.Background()
	url := "test.cloudflareaccess.com"
	access, err := NewAccessValidator(ctx, url, url, "")

	require.NoError(t, err)
	assert.NotNil(t, access)

	require.Error(t, access.Validate(ctx, ""))
	require.Error(t, access.Validate(ctx, "invalid"))

	req := httptest.NewRequest("GET", "https://test.cloudflareaccess.com", nil)
	req.Header.Set(accessJwtHeader, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c")
	assert.Error(t, access.ValidateRequest(ctx, req))
}

func TestNewAccessValidatorErr(t *testing.T) {
	ctx := context.Background()

	urls := []string{
		"",
		"ftp://test.cloudflareaccess.com",
		"wss://cloudflarenone.com",
	}

	for _, url := range urls {
		access, err := NewAccessValidator(ctx, url, url, "")

		require.Error(t, err, url)
		assert.Nil(t, access)
	}
}

func FuzzNewAccessValidator(f *testing.F) {
	f.Fuzz(func(t *testing.T, domain string, issuer string, applicationAUD string) {
		ctx := context.Background()
		_, _ = NewAccessValidator(ctx, domain, issuer, applicationAUD)
	})
}
