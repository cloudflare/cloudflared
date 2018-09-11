package validation

import (
	"fmt"
	"testing"

	"context"
	"crypto/tls"
	"crypto/x509"
	"github.com/stretchr/testify/assert"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestValidateHTTPService_HTTP2HTTP(t *testing.T) {
	server, client, err := createMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	assert.NoError(t, err)
	defer server.Close()

	assert.Equal(t, nil, ValidateHTTPService("http://example.com/", client.Transport))
}

func TestValidateHTTPService_ServerNonOKResponse(t *testing.T) {
	server, client, err := createMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))
	assert.NoError(t, err)
	defer server.Close()

	assert.Equal(t, nil, ValidateHTTPService("http://example.com/", client.Transport))
}

func TestValidateHTTPService_HTTPS2HTTP(t *testing.T) {
	server, client, err := createMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	assert.NoError(t, err)
	defer server.Close()

	assert.Equal(t,
		"example.com doesn't seem to work over https, but does seem to work over http. Consider changing the origin URL to http://example.com:1234/",
		ValidateHTTPService("https://example.com:1234/", client.Transport).Error())
}

func TestValidateHTTPService_HTTPS2HTTPS(t *testing.T) {
	server, client, err := createSecureMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	assert.NoError(t, err)
	defer server.Close()

	assert.Equal(t, nil, ValidateHTTPService("https://example.com/", client.Transport))
}

func TestValidateHTTPService_HTTP2HTTPS(t *testing.T) {
	server, client, err := createSecureMockServerAndClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	assert.NoError(t, err)
	defer server.Close()

	assert.Equal(t,
		"example.com doesn't seem to work over http, but does seem to work over https. Consider changing the origin URL to https://example.com:1234/",
		ValidateHTTPService("http://example.com:1234/", client.Transport).Error())
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
