package validation

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/stretchr/testify/assert"
)

func TestValidateHostname(t *testing.T) {
	var inputHostname string
	hostname, err := ValidateHostname(inputHostname)
	assert.Equal(t, err, nil)
	assert.Empty(t, hostname)

	inputHostname = "hello.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
	assert.Equal(t, "hello.example.com", hostname)

	inputHostname = "http://hello.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
	assert.Equal(t, "hello.example.com", hostname)

	inputHostname = "bücher.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
	assert.Equal(t, "xn--bcher-kva.example.com", hostname)

	inputHostname = "http://bücher.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
	assert.Equal(t, "xn--bcher-kva.example.com", hostname)

	inputHostname = "http%3A%2F%2Fhello.example.com"
	hostname, err = ValidateHostname(inputHostname)
	assert.Nil(t, err)
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
		assert.NoError(t, err, "test case %v", i)
		assert.Equal(t, testCase.expectedOutput, validUrl.String(), "test case %v", i)
	}

	validUrl, err := ValidateUrl("")
	assert.Equal(t, fmt.Errorf("URL should not be empty"), err)
	assert.Empty(t, validUrl)

	validUrl, err = ValidateUrl("ftp://alex:12345@hello.example.com:8080/robot.txt")
	assert.Equal(t, "Currently Cloudflare Tunnel does not support ftp protocol.", err.Error())
	assert.Empty(t, validUrl)

}

func TestNewAccessValidatorOk(t *testing.T) {
	ctx := context.Background()
	url := "test.cloudflareaccess.com"
	access, err := NewAccessValidator(ctx, url, url, "")

	assert.NoError(t, err)
	assert.NotNil(t, access)

	assert.Error(t, access.Validate(ctx, ""))
	assert.Error(t, access.Validate(ctx, "invalid"))

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

		assert.Error(t, err, url)
		assert.Nil(t, access)
	}
}

type testRoundTripper func(req *http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func emptyResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     make(http.Header),
	}
}

func createMockServerAndClient(handler http.Handler) (*httptest.Server, *http.Client, error) {
	client := http.DefaultClient
	server := httptest.NewServer(handler)

	client.Transport = &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return url.Parse(server.URL)
		},
	}

	return server, client, nil
}

func createSecureMockServerAndClient(handler http.Handler) (*httptest.Server, *http.Client, error) {
	client := http.DefaultClient
	server := httptest.NewTLSServer(handler)

	cert, err := x509.ParseCertificate(server.TLS.Certificates[0].Certificate[0])
	if err != nil {
		server.Close()
		return nil, nil, err
	}

	certpool := x509.NewCertPool()
	certpool.AddCert(cert)

	client.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("tcp", server.URL[strings.LastIndex(server.URL, "/")+1:])
		},
		TLSClientConfig: &tls.Config{
			RootCAs: certpool,
		},
	}

	return server, client, nil
}

func FuzzNewAccessValidator(f *testing.F) {
	f.Fuzz(func(t *testing.T, domain string, issuer string, applicationAUD string) {
		ctx := context.Background()
		_, _ = NewAccessValidator(ctx, domain, issuer, applicationAUD)
	})
}
