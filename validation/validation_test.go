package validation

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"time"

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

func TestToggleProtocol(t *testing.T) {
	assert.Equal(t, "https", toggleProtocol("http"))
	assert.Equal(t, "http", toggleProtocol("https"))
	assert.Equal(t, "random", toggleProtocol("random"))
	assert.Equal(t, "", toggleProtocol(""))
}

// Happy path 1: originURL is HTTP, and HTTP connections work
func TestValidateHTTPService_HTTP2HTTP(t *testing.T) {
	originURL := "http://127.0.0.1/"
	hostname := "example.com"

	assert.Nil(t, ValidateHTTPService(originURL, hostname, testRoundTripper(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, req.Host, hostname)
		if req.URL.Scheme == "http" {
			return emptyResponse(200), nil
		}
		if req.URL.Scheme == "https" {
			t.Fatal("http works, shouldn't have tried with https")
		}
		panic("Shouldn't reach here")
	})))

	assert.Nil(t, ValidateHTTPService(originURL, hostname, testRoundTripper(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, req.Host, hostname)
		if req.URL.Scheme == "http" {
			return emptyResponse(503), nil
		}
		if req.URL.Scheme == "https" {
			t.Fatal("http works, shouldn't have tried with https")
		}
		panic("Shouldn't reach here")
	})))
}

// Happy path 2: originURL is HTTPS, and HTTPS connections work
func TestValidateHTTPService_HTTPS2HTTPS(t *testing.T) {
	originURL := "https://127.0.0.1:1234/"
	hostname := "example.com"

	assert.Nil(t, ValidateHTTPService(originURL, hostname, testRoundTripper(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, req.Host, hostname)
		if req.URL.Scheme == "http" {
			t.Fatal("https works, shouldn't have tried with http")
		}
		if req.URL.Scheme == "https" {
			return emptyResponse(200), nil
		}
		panic("Shouldn't reach here")
	})))

	assert.Nil(t, ValidateHTTPService(originURL, hostname, testRoundTripper(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, req.Host, hostname)
		if req.URL.Scheme == "http" {
			t.Fatal("https works, shouldn't have tried with http")
		}
		if req.URL.Scheme == "https" {
			return emptyResponse(503), nil
		}
		panic("Shouldn't reach here")
	})))
}

// Error path 1: originURL is HTTPS, but HTTP connections work
func TestValidateHTTPService_HTTPS2HTTP(t *testing.T) {
	originURL := "https://127.0.0.1:1234/"
	hostname := "example.com"

	assert.Error(t, ValidateHTTPService(originURL, hostname, testRoundTripper(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, req.Host, hostname)
		if req.URL.Scheme == "http" {
			return emptyResponse(200), nil
		}
		if req.URL.Scheme == "https" {
			return nil, assert.AnError
		}
		panic("Shouldn't reach here")
	})))

	assert.Error(t, ValidateHTTPService(originURL, hostname, testRoundTripper(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, req.Host, hostname)
		if req.URL.Scheme == "http" {
			return emptyResponse(503), nil
		}
		if req.URL.Scheme == "https" {
			return nil, assert.AnError
		}
		panic("Shouldn't reach here")
	})))
}

// Error path 2: originURL is HTTP, but HTTPS connections work
func TestValidateHTTPService_HTTP2HTTPS(t *testing.T) {
	originURL := "http://127.0.0.1:1234/"
	hostname := "example.com"

	assert.Error(t, ValidateHTTPService(originURL, hostname, testRoundTripper(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, req.Host, hostname)
		if req.URL.Scheme == "http" {
			return nil, assert.AnError
		}
		if req.URL.Scheme == "https" {
			return emptyResponse(200), nil
		}
		panic("Shouldn't reach here")
	})))

	assert.Error(t, ValidateHTTPService(originURL, hostname, testRoundTripper(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, req.Host, hostname)
		if req.URL.Scheme == "http" {
			return nil, assert.AnError
		}
		if req.URL.Scheme == "https" {
			return emptyResponse(503), nil
		}
		panic("Shouldn't reach here")
	})))
}

// Ensure the client does not follow 302 responses
func TestValidateHTTPService_NoFollowRedirects(t *testing.T) {
	hostname := "example.com"
	redirectServer, redirectClient, err := createSecureMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/followedRedirect" {
			t.Fatal("shouldn't have followed the 302")
		}
		if r.Method == "CONNECT" {
			assert.Equal(t, "127.0.0.1:443", r.Host)
		} else {
			assert.Equal(t, hostname, r.Host)
		}
		w.Header().Set("Location", "/followedRedirect")
		w.WriteHeader(302)
	}))
	assert.NoError(t, err)
	defer redirectServer.Close()
	assert.NoError(t, ValidateHTTPService(redirectServer.URL, hostname, redirectClient.Transport))
}

// Ensure validation times out when origin URL is nonresponsive
func TestValidateHTTPService_NonResponsiveOrigin(t *testing.T) {
	originURL := "http://127.0.0.1/"
	hostname := "example.com"
	oldValidationTimeout := validationTimeout
	defer func() {
		validationTimeout = oldValidationTimeout
	}()
	validationTimeout = 500 * time.Millisecond

	// Use createMockServerAndClient, not createSecureMockServerAndClient.
	// The latter will bail with HTTP 400 immediately on an http:// request,
	// which defeats the purpose of a 'nonresponsive origin' test.
	server, client, err := createMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			assert.Equal(t, "127.0.0.1:443", r.Host)
		} else {
			assert.Equal(t, hostname, r.Host)
		}
		time.Sleep(1 * time.Second)
		w.WriteHeader(200)
	}))
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	defer server.Close()

	err = ValidateHTTPService(originURL, hostname, client.Transport)
	fmt.Println(err)
	if err, ok := err.(net.Error); assert.True(t, ok) {
		assert.True(t, err.Timeout())
	}
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
