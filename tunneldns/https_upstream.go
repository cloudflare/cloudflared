package tunneldns

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/miekg/dns"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
)

const (
	defaultTimeout = 5 * time.Second
)

// UpstreamHTTPS is the upstream implementation for DNS over HTTPS service
type UpstreamHTTPS struct {
	client   *http.Client
	endpoint *url.URL
}

// NewUpstreamHTTPS creates a new DNS over HTTPS upstream from hostname
func NewUpstreamHTTPS(endpoint string) (Upstream, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	// Update TLS and HTTP client configuration
	tls := &tls.Config{ServerName: u.Hostname()}
	transport := &http.Transport{
		TLSClientConfig:    tls,
		DisableCompression: true,
		MaxIdleConns:       1,
	}
	http2.ConfigureTransport(transport)

	client := &http.Client{
		Timeout:   defaultTimeout,
		Transport: transport,
	}

	return &UpstreamHTTPS{client: client, endpoint: u}, nil
}

// Exchange provides an implementation for the Upstream interface
func (u *UpstreamHTTPS) Exchange(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	queryBuf, err := query.Pack()
	if err != nil {
		return nil, errors.Wrap(err, "failed to pack DNS query")
	}

	// No content negotiation for now, use DNS wire format
	buf, backendErr := u.exchangeWireformat(queryBuf)
	if backendErr == nil {
		response := &dns.Msg{}
		if err := response.Unpack(buf); err != nil {
			return nil, errors.Wrap(err, "failed to unpack DNS response from body")
		}

		response.Id = query.Id
		return response, nil
	}

	log.WithError(backendErr).Errorf("failed to connect to an HTTPS backend %q", u.endpoint)
	return nil, backendErr
}

// Perform message exchange with the default UDP wireformat defined in current draft
// https://datatracker.ietf.org/doc/draft-ietf-doh-dns-over-https
func (u *UpstreamHTTPS) exchangeWireformat(msg []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", u.endpoint.String(), bytes.NewBuffer(msg))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create an HTTPS request")
	}

	req.Header.Add("Content-Type", "application/dns-udpwireformat")
	req.Host = u.endpoint.Hostname()

	resp, err := u.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to perform an HTTPS request")
	}

	// Check response status code
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("returned status code %d", resp.StatusCode)
	}

	// Read wireformat response from the body
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the response body")
	}

	return buf, nil
}
