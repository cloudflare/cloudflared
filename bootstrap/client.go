// Package bootstrap retrieves the metadata needed to connect a tunnel to the edge.
package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/credentials"
	"github.com/cloudflare/cloudflared/retry"
)

const (
	globalBaseURL       = "https://api.cftunnel.com"
	fedBaseURL          = "https://api.fed.cftunnel.com"
	bootstrapPath       = "v1/tunnel/bootstrap"
	bootstrapClientType = "cloudflared"
	responseLimit       = 64 * 1024
)

type bootstrapRequest struct {
	Client bootstrapClient `json:"client"`
}

type bootstrapClient struct {
	Type        string `json:"type"`
	Version     string `json:"version"`
	OSArch      string `json:"os_arch"`
	ConnectorID string `json:"connector_id"`
}

// Regionalization contains the assigned region for a regionalized tunnel.
type Regionalization *struct {
	RegionKey     string `json:"region_key"`
	RegionShortID uint32 `json:"region_short_id"`
}

// Response contains the tunnel identity and current edge connection settings.
type Response struct {
	AccountTag      string            `json:"account_tag"`
	TunnelID        uuid.UUID         `json:"tunnel_id"`
	SNI             map[string]string `json:"sni"`
	Regionalization Regionalization   `json:"regionalization"`
}

// Client fetches bootstrap metadata from the Tunnel Bootstrap Worker.
type Client struct {
	baseURL           string
	httpClient        *http.Client
	maxBackoffRetries uint
	log               *zerolog.Logger
}

// NewClient creates a client for the deployment specified by the tunnel
// credentials' endpoint. Endpoints other than Fed use the global deployment.
// maxBackoffRetries controls how far the retry delay can grow. Zero caps the
// delay at the base interval; it does not disable retries.
func NewClient(endpoint string, maxBackoffRetries uint, log *zerolog.Logger) *Client {
	baseURL := globalBaseURL
	if endpoint == credentials.FedEndpoint {
		baseURL = fedBaseURL
	}
	return newClient(baseURL, maxBackoffRetries, log)
}

func newClient(baseURL string, maxBackoffRetries uint, log *zerolog.Logger) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:       5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		maxBackoffRetries: maxBackoffRetries,
		log:               log,
	}
}

// FetchMetadata fetches usable QUIC bootstrap metadata, retrying until
// validation succeeds or the context is canceled.
func (c *Client) FetchMetadata(ctx context.Context, creds connection.Credentials, config *client.Config) (*Response, error) {
	backoff := retry.NewBackoff(c.maxBackoffRetries, retry.DefaultBaseTime, true)
	for ctx.Err() == nil {
		result, err := c.fetchMetadata(ctx, creds, config)
		if err == nil {
			err = result.Validate(creds)
		}
		recordAttempt(ctx, err)
		if ctx.Err() != nil {
			break
		}
		if err == nil {
			return result, nil
		}
		c.log.Warn().Err(err).Msg("unable to refresh bootstrap after account region change")
		if !backoff.Backoff(ctx) {
			break
		}
	}
	return nil, ctx.Err()
}

// fetchMetadata fetches and decodes bootstrap metadata using the tunnel credentials.
func (c *Client) fetchMetadata(ctx context.Context, creds connection.Credentials, config *client.Config) (*Response, error) {
	token, err := connection.TunnelToken(creds).Encode()
	if err != nil {
		return nil, bootstrapErrorf(outcomeTokenEncodeError, "encode bootstrap tunnel token: %w", err)
	}
	body, err := json.Marshal(bootstrapRequest{
		Client: bootstrapClient{
			Type:        bootstrapClientType,
			Version:     config.Version,
			OSArch:      config.Arch,
			ConnectorID: config.ConnectorID.String(),
		},
	})
	if err != nil {
		return nil, bootstrapErrorf(outcomeRequestSetupError, "encode bootstrap request: %w", err)
	}
	requestURL, err := url.JoinPath(c.baseURL, bootstrapPath)
	if err != nil {
		return nil, bootstrapErrorf(outcomeRequestSetupError, "join bootstrap URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, bootstrapErrorf(outcomeRequestSetupError, "create bootstrap request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("cloudflared/%s", config.Version))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, bootstrapErrorf(outcomeTransportError, "fetch bootstrap: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, bootstrapErrorf(httpOutcome(resp.StatusCode), "bootstrap returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit+1))
	if err != nil {
		return nil, bootstrapErrorf(outcomeTransportError, "read bootstrap: %w", err)
	}
	if len(data) > responseLimit {
		return nil, &attemptError{outcome: outcomeInvalidResponse, err: errors.New("bootstrap response exceeds size limit")}
	}
	var result Response
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, bootstrapErrorf(outcomeInvalidResponse, "decode bootstrap: %w", err)
	}
	return &result, nil
}

// Validate checks that the response is usable for reconnecting the given tunnel over QUIC.
func (r *Response) Validate(creds connection.Credentials) error {
	if r == nil {
		return &attemptError{outcome: outcomeValidationError, err: errors.New("bootstrap response is missing")}
	}
	if r.AccountTag != creds.AccountTag || r.TunnelID != creds.TunnelID {
		return &attemptError{outcome: outcomeValidationError, err: errors.New("bootstrap identity does not match tunnel")}
	}
	if r.SNI[connection.QUIC.String()] == "" {
		return &attemptError{outcome: outcomeValidationError, err: errors.New("bootstrap response is missing QUIC SNI")}
	}
	return nil
}
