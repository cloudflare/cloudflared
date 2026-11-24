package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/connection"
)

func TestBootstrapAttemptOutcomes(t *testing.T) {
	// The cases measure deltas on process-wide counters, so run them serially.
	for _, tc := range []struct {
		name         string
		wantOutcome  string
		status       int
		body         string
		transportErr error
		baseURL      string
		cancel       bool
	}{
		{name: "valid response", wantOutcome: "success", status: http.StatusOK, body: `{"sni":{"quic":"edge.example"}}`},
		{name: "invalid base URL", wantOutcome: "other_request_setup_error", baseURL: "://invalid"},
		{name: "HTTP redirect", wantOutcome: "http_other", status: http.StatusTemporaryRedirect},
		{name: "HTTP 400", wantOutcome: "http_4xx", status: http.StatusBadRequest},
		{name: "HTTP 499", wantOutcome: "http_4xx", status: 499},
		{name: "HTTP 500", wantOutcome: "http_5xx", status: http.StatusInternalServerError},
		{name: "HTTP 599", wantOutcome: "http_5xx", status: 599},
		{name: "HTTP 600", wantOutcome: "http_other", status: 600},
		{name: "malformed JSON", wantOutcome: "invalid_response", status: http.StatusOK, body: `{`},
		{name: "missing QUIC SNI", wantOutcome: "validation_error", status: http.StatusOK, body: `{}`},
		{name: "request timeout with live parent", wantOutcome: "transport_error", transportErr: context.DeadlineExceeded},
		{name: "parent canceled", wantOutcome: "canceled", transportErr: context.Canceled, cancel: true},
		{name: "unrelated error after cancellation", wantOutcome: "transport_error", transportErr: io.ErrUnexpectedEOF, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			c := newTestClient()
			if tc.baseURL != "" {
				log := zerolog.Nop()
				c = newClient(tc.baseURL, 1, &log)
			}
			c.httpClient.Transport = bootstrapRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				defer func() { require.NoError(t, req.Body.Close()) }()
				if tc.transportErr != nil {
					return nil, tc.transportErr
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})

			creds := connection.Credentials{}
			result, err := c.fetchMetadata(ctx, creds, &client.Config{})
			if err == nil {
				err = result.Validate(creds)
			}
			requireAttemptIncrement(t, ctx, err, tc.wantOutcome)
		})
	}
}

func TestRecordAttemptWrappedAndUnclassifiedErrors(t *testing.T) {
	cause := errors.New("encoding failed")
	classified := bootstrapErrorf(outcomeTokenEncodeError, "encode bootstrap tunnel token: %w", cause)
	require.EqualError(t, classified, "encode bootstrap tunnel token: encoding failed")
	require.ErrorIs(t, classified, cause)

	requireAttemptIncrement(t, t.Context(), fmt.Errorf("bootstrap failed: %w", classified), "token_encode_error")
	requireAttemptIncrement(t, t.Context(), cause, "other_error")
}

func requireAttemptIncrement(t *testing.T, ctx context.Context, err error, outcome string) {
	t.Helper()
	before := attemptCount(t, outcome)
	recordAttempt(ctx, err)
	require.InDelta(t, before+1, attemptCount(t, outcome), 0)
}

func attemptCount(t *testing.T, outcome string) float64 {
	t.Helper()
	var metric dto.Metric
	require.NoError(t, attempts.WithLabelValues(outcome).Write(&metric))
	return metric.GetCounter().GetValue()
}
