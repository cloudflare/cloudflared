package tunneldns

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudflare/cloudflared/logger"
	odoh "github.com/cloudflare/odoh-go"
	"github.com/miekg/dns"
	"github.com/pkg/errors"
	"golang.org/x/net/http2"
)

const (
	defaultTimeout     = 10 * time.Second
	odohConfigDuration = 3600 * time.Second
	targetHostname     = "odoh.cloudflare-dns.com."
)

// ObliviousDoHCtx maintains info needed for the ODoH service
type ObliviousDoHCtx struct {
	useproxy bool
	target   *url.URL
	queryCtx *odoh.QueryContext
}

// UpstreamHTTPS is the upstream implementation for DNS over HTTPS service
type UpstreamHTTPS struct {
	client     *http.Client
	endpoint   *url.URL
	bootstraps []string
	odoh       *ObliviousDoHCtx
	logger     logger.Service
}

// NewUpstreamHTTPS creates a new DNS over HTTPS upstream from endpoint
func NewUpstreamHTTPS(endpoint string, bootstraps []string, odohCtx *ObliviousDoHCtx, logger logger.Service) (Upstream, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	return &UpstreamHTTPS{
		client:     configureClient(u.Hostname()),
		endpoint:   u,
		bootstraps: bootstraps,
		odoh:       odohCtx,
		logger:     logger,
	}, nil
}

// Exchange provides an implementation for the Upstream interface
func (u *UpstreamHTTPS) Exchange(ctx context.Context, query *dns.Msg) (*dns.Msg, error) {
	queryBuf, err := query.Pack()
	if err != nil {
		return nil, errors.Wrap(err, "failed to pack DNS query")
	}

	if u.odoh == nil {
		if len(query.Question) > 0 && query.Question[0].Name == fmt.Sprintf("%s.", u.endpoint.Hostname()) {
			for _, bootstrap := range u.bootstraps {
				endpoint, client, err := configureBootstrap(bootstrap)
				if err != nil {
					u.logger.Errorf("failed to configure boostrap upstream %s: %s", bootstrap, err)
					continue
				}
				msg, err := exchange(queryBuf, query.Id, endpoint, client, u.odoh, u.logger)
				if err != nil {
					u.logger.Errorf("failed to connect to a boostrap upstream %s: %s", bootstrap, err)
					continue
				}
				return msg, nil
			}
			return nil, fmt.Errorf("failed to reach any bootstrap upstream: %v", u.bootstraps)
		}
	} else {
		odohQuery, queryCtx, err := createOdohQuery(queryBuf, OdohConfig)
		if err != nil {
			u.logger.Errorf("failed to create oblivious query: %s", err)
		}
		queryBuf = odohQuery
		u.odoh.queryCtx = &queryCtx
	}
	return exchange(queryBuf, query.Id, u.endpoint, u.client, u.odoh, u.logger)

}

func createOdohQuery(dnsMessage []byte, publicKey odoh.ObliviousDoHConfigContents) ([]byte, odoh.QueryContext, error) {
	odohQuery := odoh.CreateObliviousDNSQuery(dnsMessage, 0)
	encryptedMessage, queryContext, err := publicKey.EncryptQuery(odohQuery)
	if err != nil {
		return nil, odoh.QueryContext{}, err
	}
	return encryptedMessage.Marshal(), queryContext, nil
}

// FetchObliviousDoHConfig fetches `odohconfig` by querying the target server for HTTPS records.
func FetchObliviousDoHConfig(client *http.Client, msg []byte, dohResolver *url.URL) (*odoh.ObliviousDoHConfigs, error) {
	buf, err := exchangeWireformat(msg, dohResolver, client, nil)
	if err != nil {
		return nil, err
	}

	response := &dns.Msg{}
	if err := response.Unpack(buf); err != nil {
		return nil, errors.Wrap(err, "failed to unpack HTTPS DNS response from body")
	}

	// extracts `odohconfig` from the https record
	for _, answer := range response.Answer {
		httpsResponse, ok := answer.(*dns.HTTPS)
		if ok {
			for _, value := range httpsResponse.Value {
				if value.Key() == 32769 {
					parameter, ok := value.(*dns.SVCBLocal)
					if ok {
						odohConfigs, err := odoh.UnmarshalObliviousDoHConfigs(parameter.Data)
						if err == nil {
							return &odohConfigs, nil
						}
					}
				}
			}
		}
	}

	return nil, nil
}

func exchange(msg []byte, queryID uint16, endpoint *url.URL, client *http.Client, odohCtx *ObliviousDoHCtx, logger logger.Service) (*dns.Msg, error) {
	// No content negotiation for now, use DNS wire format
	buf, backendErr := exchangeWireformat(msg, endpoint, client, odohCtx)
	if backendErr == nil {
		response := &dns.Msg{}
		if odohCtx != nil {
			odohQueryResponse, err := odoh.UnmarshalDNSMessage(buf)
			if err != nil {
				return nil, errors.Wrap(err, "failed to deserialize ObliviousDoHMessage from response")
			}
			buf, err = odohCtx.queryCtx.OpenAnswer(odohQueryResponse)
			if err != nil {
				return nil, errors.Wrap(err, "failed to decrypt encrypted response")
			}
		}
		if err := response.Unpack(buf); err != nil {
			return nil, errors.Wrap(err, "failed to unpack DNS response from body")
		}
		response.Id = queryID
		return response, nil
	}

	logger.Errorf("failed to connect to an HTTPS backend %q: %s", endpoint, backendErr)
	return nil, backendErr
}

// Perform message exchange with the default UDP wireformat defined in current draft
// https://datatracker.ietf.org/doc/draft-ietf-doh-dns-over-https for DoH and
// https://tools.ietf.org/html/draft-pauly-dprive-oblivious-doh-03 for ODoH
func exchangeWireformat(msg []byte, endpoint *url.URL, client *http.Client, odoh *ObliviousDoHCtx) ([]byte, error) {
	req, err := http.NewRequest("POST", endpoint.String(), bytes.NewBuffer(msg))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create an HTTPS request")
	}

	if odoh != nil {
		req.Header.Add("Content-Type", "application/oblivious-dns-message")
		req.Header.Add("Accept", "application/oblivious-dns-message")
		req.Header.Add("Cache-Control", "no-cache, no-store")
		if odoh.useproxy {
			queries := req.URL.Query()
			queries.Add("targethost", odoh.target.Hostname())
			queries.Add("targetpath", "/dns-query")
			req.URL.RawQuery = queries.Encode()
		}
	} else {
		req.Header.Add("Content-Type", "application/dns-message")
	}
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
	buf, err := ioutil.ReadAll(resp.Body)
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

	return b, configureClient(b.Hostname()), nil
}

// configureClient will configure a HTTPS client for upstream DoH requests
func configureClient(hostname string) *http.Client {
	// Update TLS and HTTP client configuration
	tls := &tls.Config{ServerName: hostname}
	transport := &http.Transport{
		TLSClientConfig:    tls,
		DisableCompression: true,
		MaxIdleConns:       1,
		Proxy:              http.ProxyFromEnvironment,
	}
	http2.ConfigureTransport(transport)

	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: transport,
	}
}
