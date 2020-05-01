# GoCertifi: SSL Certificates for Golang

This Go package contains a CA bundle that you can reference in your Go code.
This is useful for systems that do not have CA bundles that Golang can find
itself, or where a uniform set of CAs is valuable.

This is the same CA bundle that ships with the
[Python Requests](https://github.com/kennethreitz/requests) library, and is a
Golang specific port of [certifi](https://github.com/kennethreitz/certifi). The
CA bundle is derived from Mozilla's canonical set.

## Usage

You can use the `gocertifi` package as follows:

```go
import "github.com/certifi/gocertifi"

certPool, err := gocertifi.CACerts()
```

You can use the returned `*x509.CertPool` as part of an HTTP transport, for example:

```go
import (
  "net/http"
  "crypto/tls"
)

// Setup an HTTP client with a custom transport
transport := &http.Transport{
  Proxy: ProxyFromEnvironment,
  DialContext: (&net.Dialer{
    Timeout:   30 * time.Second,
    KeepAlive: 30 * time.Second,
    DualStack: true,
  }).DialContext,
  ForceAttemptHTTP2:     true,
  MaxIdleConns:          100,
  IdleConnTimeout:       90 * time.Second,
  TLSHandshakeTimeout:   10 * time.Second,
  ExpectContinueTimeout: 1 * time.Second,
}
// or, starting with go1.13 simply use:
// transport := http.DefaultTransport.(*http.Transport).Clone()

transport.TLSClientConfig = &tls.Config{RootCAs: certPool}
client := &http.Client{Transport: transport}

// Make an HTTP request using our custom transport
resp, err := client.Get("https://example.com")
```

## Detailed Documentation

Import as follows:

```go
import "github.com/certifi/gocertifi"
```

### Errors

```go
var ErrParseFailed = errors.New("gocertifi: error when parsing certificates")
```

### Functions

```go
func CACerts() (*x509.CertPool, error)
```
CACerts builds an X.509 certificate pool containing the Mozilla CA Certificate
bundle. Returns nil on error along with an appropriate error code.
