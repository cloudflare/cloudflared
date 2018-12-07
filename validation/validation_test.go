package validation

import (
	"bytes"
	"fmt"
	"io/ioutil"
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
	validUrl, err := ValidateUrl("")
	assert.Equal(t, fmt.Errorf("URL should not be empty"), err)
	assert.Empty(t, validUrl)

	validUrl, err = ValidateUrl("https://localhost:8080")
	assert.Nil(t, err)
	assert.Equal(t, "https://localhost:8080", validUrl)

	validUrl, err = ValidateUrl("localhost:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://localhost:8080", validUrl)

	validUrl, err = ValidateUrl("http://localhost")
	assert.Nil(t, err)
	assert.Equal(t, "http://localhost", validUrl)

	validUrl, err = ValidateUrl("http://127.0.0.1:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://127.0.0.1:8080", validUrl)

	validUrl, err = ValidateUrl("127.0.0.1:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://127.0.0.1:8080", validUrl)

	validUrl, err = ValidateUrl("127.0.0.1")
	assert.Nil(t, err)
	assert.Equal(t, "http://127.0.0.1", validUrl)

	validUrl, err = ValidateUrl("https://127.0.0.1:8080")
	assert.Nil(t, err)
	assert.Equal(t, "https://127.0.0.1:8080", validUrl)

	validUrl, err = ValidateUrl("[::1]:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://[::1]:8080", validUrl)

	validUrl, err = ValidateUrl("http://[::1]")
	assert.Nil(t, err)
	assert.Equal(t, "http://[::1]", validUrl)

	validUrl, err = ValidateUrl("http://[::1]:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://[::1]:8080", validUrl)

	validUrl, err = ValidateUrl("[::1]")
	assert.Nil(t, err)
	assert.Equal(t, "http://[::1]", validUrl)

	validUrl, err = ValidateUrl("https://example.com")
	assert.Nil(t, err)
	assert.Equal(t, "https://example.com", validUrl)

	validUrl, err = ValidateUrl("example.com")
	assert.Nil(t, err)
	assert.Equal(t, "http://example.com", validUrl)

	validUrl, err = ValidateUrl("http://hello.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "http://hello.example.com", validUrl)

	validUrl, err = ValidateUrl("hello.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "http://hello.example.com", validUrl)

	validUrl, err = ValidateUrl("hello.example.com:8080")
	assert.Nil(t, err)
	assert.Equal(t, "http://hello.example.com:8080", validUrl)

	validUrl, err = ValidateUrl("https://hello.example.com:8080")
	assert.Nil(t, err)
	assert.Equal(t, "https://hello.example.com:8080", validUrl)

	validUrl, err = ValidateUrl("https://bücher.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "https://xn--bcher-kva.example.com", validUrl)

	validUrl, err = ValidateUrl("bücher.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "http://xn--bcher-kva.example.com", validUrl)

	validUrl, err = ValidateUrl("https%3A%2F%2Fhello.example.com")
	assert.Nil(t, err)
	assert.Equal(t, "https://hello.example.com", validUrl)

	validUrl, err = ValidateUrl("ftp://alex:12345@hello.example.com:8080/robot.txt")
	assert.Equal(t, "Currently Argo Tunnel does not support ftp protocol.", err.Error())
	assert.Empty(t, validUrl)

	validUrl, err = ValidateUrl("https://alex:12345@hello.example.com:8080")
	assert.Nil(t, err)
	assert.Equal(t, "https://hello.example.com:8080", validUrl)

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

	// Integration-style test with a mock server
	server, client, err := createMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, hostname, r.Host)
		w.WriteHeader(200)
	}))
	assert.NoError(t, err)
	defer server.Close()
	assert.Nil(t, ValidateHTTPService(originURL, hostname, client.Transport))

	// this will fail if the client follows the 302
	redirectServer, redirectClient, err := createMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	assert.Nil(t, ValidateHTTPService(originURL, hostname, redirectClient.Transport))

}

// Happy path 2: originURL is HTTPS, and HTTPS connections work
func TestValidateHTTPService_HTTPS2HTTPS(t *testing.T) {
	originURL := "https://127.0.0.1/"
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

	// Integration-style test with a mock server
	server, client, err := createSecureMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			assert.Equal(t, "127.0.0.1:443", r.Host)
		} else {
			assert.Equal(t, hostname, r.Host)
		}
		w.WriteHeader(200)
	}))
	assert.NoError(t, err)
	defer server.Close()
	assert.Nil(t, ValidateHTTPService(originURL, hostname, client.Transport))

	// this will fail if the client follows the 302
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
	assert.Nil(t, ValidateHTTPService(originURL, hostname, redirectClient.Transport))
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

	// Integration-style test with a mock server
	server, client, err := createMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			assert.Equal(t, "127.0.0.1:1234", r.Host)
		} else {
			assert.Equal(t, hostname, r.Host)
		}
		w.WriteHeader(200)
	}))
	assert.NoError(t, err)
	defer server.Close()
	assert.Error(t, ValidateHTTPService(originURL, hostname, client.Transport))

	// this will fail if the client follows the 302
	redirectServer, redirectClient, err := createMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/followedRedirect" {
			t.Fatal("shouldn't have followed the 302")
		}
		if r.Method == "CONNECT" {
			assert.Equal(t, "127.0.0.1:1234", r.Host)
		} else {
			assert.Equal(t, hostname, r.Host)
		}
		w.Header().Set("Location", "/followedRedirect")
		w.WriteHeader(302)
	}))
	assert.NoError(t, err)
	defer redirectServer.Close()
	assert.Error(t, ValidateHTTPService(originURL, hostname, redirectClient.Transport))

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

	// Integration-style test with a mock server
	server, client, err := createSecureMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "CONNECT" {
			assert.Equal(t, "127.0.0.1:1234", r.Host)
		} else {
			assert.Equal(t, hostname, r.Host)
		}
		w.WriteHeader(200)
	}))
	assert.NoError(t, err)
	defer server.Close()
	assert.Error(t, ValidateHTTPService(originURL, hostname, client.Transport))

	// this will fail if the client follows the 302
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
	assert.Error(t, ValidateHTTPService(originURL, hostname, redirectClient.Transport))
}

type testRoundTripper func(req *http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func emptyResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       ioutil.NopCloser(bytes.NewReader(nil)),
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
