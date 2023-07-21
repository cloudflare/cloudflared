package tunneldns

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/miekg/dns"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/http2"
)

const (
	defaultTimeout = 5 * time.Second
)

// UpstreamHTTPS is the upstream implementation for DNS over HTTPS service
type UpstreamHTTPS struct {
	client     *http.Client
	endpoint   *url.URL
	bootstraps []string
	log        *zerolog.Logger
}

// NewUpstreamHTTPS creates a new DNS over HTTPS upstream from endpoint
func NewUpstreamHTTPS(endpoint string, bootstraps []string, maxConnections int, log *zerolog.Logger) (Upstream, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	return &UpstreamHTTPS{client: configureClient(u.Hostname(), maxConnections), endpoint: u, bootstraps: bootstraps, log: log}, nil
}

// Exchange provides an implementation for the Upstream interface
func (u *UpstreamHTTPS) Exchange(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	queryBuf, err := query.Pack()
	if err != nil {
		return nil, errors.Wrap(err, "failed to pack DNS query")
	}

	if len(query.Question) > 0 && query.Question[0].Name == fmt.Sprintf("%s.", u.endpoint.Hostname()) {
		for _, bootstrap := range u.bootstraps {
			endpoint, client, err := configureBootstrap(bootstrap)
			if err != nil {
				u.log.Err(err).Msgf("failed to configure bootstrap upstream %s", bootstrap)
				continue
			}
			msg, err := exchange(queryBuf, query.Id, endpoint, client, u.log)
			if err != nil {
				u.log.Err(err).Msgf("failed to connect to a bootstrap upstream %s", bootstrap)
				continue
			}
			return msg, nil
		}
		return nil, fmt.Errorf("failed to reach any bootstrap upstream: %v", u.bootstraps)
	}

	return exchange(queryBuf, query.Id, u.endpoint, u.client, u.log)
}

func exchange(msg []byte, queryID uint16, endpoint *url.URL, client *http.Client, log *zerolog.Logger) (*dns.Msg, error) {
	// No content negotiation for now, use DNS wire format
	buf, backendErr := exchangeWireformat(msg, endpoint, client)
	if backendErr == nil {
		response := &dns.Msg{}
		if err := response.Unpack(buf); err != nil {
			return nil, errors.Wrap(err, "failed to unpack DNS response from body")
		}

		response.Id = queryID
		return response, nil
	}

	log.Err(backendErr).Msgf("failed to connect to an HTTPS backend %q", endpoint)
	return nil, backendErr
}

// Perform message exchange with the default UDP wireformat defined in current draft
// https://datatracker.ietf.org/doc/draft-ietf-doh-dns-over-https
func exchangeWireformat(msg []byte, endpoint *url.URL, client *http.Client) ([]byte, error) {
	req, err := http.NewRequest("POST", endpoint.String(), bytes.NewBuffer(msg))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create an HTTPS request")
	}

	req.Header.Add("Content-Type", "application/dns-message")
	req.Host = endpoint.Host

	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to perform an HTTPS request")
	}

	// Check response status code
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("returned status code %d", resp.StatusCode)
	}

	// Read wireformat response from the body
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the response body")
	}

	return buf, nil
}

func configureBootstrap(bootstrap string) (*url.URL, *http.Client, error) {
	b, err := url.Parse(bootstrap)
	if err != nil {
		return nil, nil, err
	}
	if ip := net.ParseIP(b.Hostname()); ip == nil {
		return nil, nil, fmt.Errorf("bootstrap address of %s must be an IP address", b.Hostname())
	}

	return b, configureClient(b.Hostname(), MaxUpstreamConnsDefault), nil
}

// configureClient will configure a HTTPS client for upstream DoH requests
func configureClient(hostname string, maxUpstreamConnections int) *http.Client {
	// Update TLS and HTTP client configuration
	tlsConfig := &tls.Config{ServerName: hostname}
	transport := &http.Transport{
		TLSClientConfig:    tlsConfig,
		DisableCompression: true,
		MaxIdleConns:       1,
		MaxConnsPerHost:    maxUpstreamConnections,
		Proxy:              http.ProxyFromEnvironment,
	}
	_ = http2.ConfigureTransport(transport)

	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: transport,
	}
}
