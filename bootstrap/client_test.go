package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/credentials"
)

type fetchTestCase struct {
	name, body string
	status     int
	wantErr    string
}

func TestFetch(t *testing.T) {
	t.Parallel()
	creds := connection.Credentials{AccountTag: "account", TunnelID: uuid.New(), TunnelSecret: []byte("secret"), Endpoint: credentials.FedEndpoint}
	config := &client.Config{Version: "test", Arch: "darwin_arm64", ConnectorID: uuid.New()}
	const userAgent = "cloudflared/test"
	token, err := connection.TunnelToken(creds).Encode()
	require.NoError(t, err)
	for _, tc := range []fetchTestCase{
		{"success", `{"account_tag":"account","tunnel_id":"` + creds.TunnelID.String() + `","sni":{"quic":"ch.example","http2":"ch.example"},"regionalization":{"region_key":"ch","region_short_id":1}}`, http.StatusOK, ""},
		{"QUIC only", `{"account_tag":"account","tunnel_id":"` + creds.TunnelID.String() + `","sni":{"quic":"ch.example"}}`, http.StatusOK, ""},
		{"global", `{"account_tag":"account","tunnel_id":"` + creds.TunnelID.String() + `","sni":{"quic":"quic.cftunnel.com","http2":"h2.cftunnel.com"},"regionalization":null}`, http.StatusOK, ""},
		{"decodes metadata without validating it", `{"account_tag":"other","tunnel_id":"` + uuid.NewString() + `"}`, http.StatusOK, ""},
		{"malformed", `{`, http.StatusOK, "decode bootstrap: unexpected end of JSON input"},
		{"too large", strings.Repeat("x", responseLimit+1), http.StatusOK, "bootstrap response exceeds size limit"},
		{"unauthorized", "", http.StatusUnauthorized, "bootstrap returned HTTP 401"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fetchTestBody(t, config, creds, userAgent, token, tc)
		})
	}
}

func fetchTestBody(t *testing.T, config *client.Config, creds connection.Credentials, userAgent, token string, tc fetchTestCase) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/tunnel/bootstrap", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, userAgent, r.Header.Get("User-Agent"))
		assert.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
		var body map[string]map[string]string
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, map[string]string{
			"type": "cloudflared", "version": config.Version,
			"os_arch": config.Arch, "connector_id": config.ConnectorID.String(),
		}, body["client"])
		w.WriteHeader(tc.status)
		_, err := w.Write([]byte(tc.body))
		assert.NoError(t, err)
	}))
	defer server.Close()
	log := zerolog.Nop()
	bootstrapClient := newClient(server.URL+"/", 1, &log)
	result, err := bootstrapClient.fetchMetadata(t.Context(), creds, config)
	if tc.wantErr != "" {
		require.ErrorContains(t, err, tc.wantErr)
		require.Nil(t, result)
	} else {
		require.NoError(t, err)
		var expected Response
		require.NoError(t, json.Unmarshal([]byte(tc.body), &expected))
		require.Equal(t, &expected, result)
	}
	require.Equal(t, int32(1), requests.Load())
}

func TestResponseValidate(t *testing.T) {
	t.Parallel()

	creds := connection.Credentials{AccountTag: "account", TunnelID: uuid.New()}
	quicSNI := map[string]string{"quic": "edge.example"}
	for _, tc := range []struct {
		name     string
		response *Response
		wantErr  string
	}{
		{"valid", &Response{AccountTag: creds.AccountTag, TunnelID: creds.TunnelID, SNI: quicSNI}, ""},
		{"missing response", nil, "bootstrap response is missing"},
		{"wrong account", &Response{AccountTag: "other", TunnelID: creds.TunnelID, SNI: quicSNI}, "bootstrap identity does not match tunnel"},
		{"wrong tunnel", &Response{AccountTag: creds.AccountTag, TunnelID: uuid.New(), SNI: quicSNI}, "bootstrap identity does not match tunnel"},
		{"missing SNI", &Response{AccountTag: creds.AccountTag, TunnelID: creds.TunnelID}, "bootstrap response is missing QUIC SNI"},
		{"missing QUIC SNI", &Response{AccountTag: creds.AccountTag, TunnelID: creds.TunnelID, SNI: map[string]string{"http2": "edge.example"}}, "bootstrap response is missing QUIC SNI"},
		{"empty QUIC SNI", &Response{AccountTag: creds.AccountTag, TunnelID: creds.TunnelID, SNI: map[string]string{"quic": ""}}, "bootstrap response is missing QUIC SNI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.response.Validate(creds)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFetchDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	log := zerolog.Nop()
	bootstrapClient := newClient(server.URL, 1, &log)
	_, err := bootstrapClient.fetchMetadata(t.Context(), connection.Credentials{}, &client.Config{})
	require.ErrorContains(t, err, "HTTP 307")
	require.False(t, redirected.Load())
}

func TestFetchInvalidURL(t *testing.T) {
	t.Parallel()
	log := zerolog.Nop()
	bootstrapClient := newClient("://invalid", 1, &log)
	_, err := bootstrapClient.fetchMetadata(t.Context(), connection.Credentials{}, &client.Config{})
	require.ErrorContains(t, err, "join bootstrap URL:")
}

func TestFetchUsesDeploymentFromCredentialEndpoint(t *testing.T) {
	t.Parallel()

	log := zerolog.Nop()
	for _, tc := range []struct {
		name     string
		endpoint string
		wantURL  string
	}{
		{"default", "", "https://api.cftunnel.com/v1/tunnel/bootstrap"},
		{"fed", credentials.FedEndpoint, "https://api.fed.cftunnel.com/v1/tunnel/bootstrap"},
		{"other endpoint", "custom", "https://api.cftunnel.com/v1/tunnel/bootstrap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bootstrapClient := NewClient(tc.endpoint, 1, &log)
			bootstrapClient.httpClient.Transport = bootstrapRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				defer func() { require.NoError(t, req.Body.Close()) }()
				assert.Equal(t, tc.wantURL, req.URL.String())
				return bootstrapTestResponse(http.StatusOK, `{}`), nil
			})
			_, err := bootstrapClient.fetchMetadata(t.Context(), connection.Credentials{Endpoint: tc.endpoint}, &client.Config{})
			require.NoError(t, err)
		})
	}
}

func TestFetchMetadataWaitsForValidMetadata(t *testing.T) {
	// This test measures deltas on process-wide counters.
	before := map[string]float64{
		outcomeHTTP5xx:         attemptCount(t, outcomeHTTP5xx),
		outcomeValidationError: attemptCount(t, outcomeValidationError),
		outcomeSuccess:         attemptCount(t, outcomeSuccess),
	}
	synctest.Test(t, func(t *testing.T) {
		bootstrapClient := newTestClient()
		creds := connection.Credentials{AccountTag: "account", TunnelID: uuid.New()}
		var attempts int
		bootstrapClient.httpClient.Transport = bootstrapRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			switch attempts {
			case 1:
				return bootstrapTestResponse(http.StatusServiceUnavailable, ""), nil
			case 2:
				return bootstrapTestResponse(http.StatusOK, `{"account_tag":"account","tunnel_id":"`+creds.TunnelID.String()+`"}`), nil
			default:
				return bootstrapTestResponse(http.StatusOK, `{"account_tag":"account","tunnel_id":"`+creds.TunnelID.String()+`","sni":{"quic":"ch.example"}}`), nil
			}
		})
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		result, err := bootstrapClient.FetchMetadata(ctx, creds, &client.Config{})
		require.NoError(t, err)
		require.Equal(t, 3, attempts, "the backoff cap must not limit the number of attempts")
		require.Equal(t, "ch.example", result.SNI["quic"])
	})
	for outcome, count := range before {
		require.InDelta(t, count+1, attemptCount(t, outcome), 0, outcome)
	}
}

func TestFetchMetadataRecordsCanceledAttempt(t *testing.T) {
	// This test measures deltas on process-wide counters.
	before := attemptCount(t, outcomeCanceled)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := newTestClient()
	c.httpClient.Transport = bootstrapRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		defer func() { require.NoError(t, req.Body.Close()) }()
		cancel()
		return nil, req.Context().Err()
	})

	_, err := c.FetchMetadata(ctx, connection.Credentials{}, &client.Config{})
	require.ErrorIs(t, err, context.Canceled)
	require.InDelta(t, before+1, attemptCount(t, outcomeCanceled), 0)
}

func TestFetchMetadataCancellationInterruptsBackoff(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		bootstrapClient := newTestClient()
		var attempts int
		bootstrapClient.httpClient.Transport = bootstrapRoundTripperFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return bootstrapTestResponse(http.StatusServiceUnavailable, ""), nil
		})
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, err := bootstrapClient.FetchMetadata(ctx, connection.Credentials{}, &client.Config{})
			done <- err
		}()

		// Wait for the first failure and pending retry without advancing virtual time.
		synctest.Wait()
		require.Equal(t, 1, attempts)
		stoppedAt := time.Now()
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		require.Equal(t, stoppedAt, time.Now(), "cancellation must not wait for the retry timer")
		require.Equal(t, 1, attempts, "cancellation must prevent another request")
	})
}

func newTestClient() *Client {
	log := zerolog.Nop()
	return NewClient("", 1, &log)
}

func bootstrapTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

type bootstrapRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f bootstrapRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
