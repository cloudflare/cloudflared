package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/cloudflare/cloudflared/connection"
)

const (
	outcomeSuccess           = "success"
	outcomeCanceled          = "canceled"
	outcomeTokenEncodeError  = "token_encode_error" // #nosec G101 -- Prometheus outcome label, not a credential.
	outcomeRequestSetupError = "other_request_setup_error"
	outcomeTransportError    = "transport_error"
	outcomeHTTP4xx           = "http_4xx"
	outcomeHTTP5xx           = "http_5xx"
	outcomeHTTPOther         = "http_other"
	outcomeInvalidResponse   = "invalid_response"
	outcomeValidationError   = "validation_error"
	outcomeOtherError        = "other_error"
)

var attempts = promauto.NewCounterVec(prometheus.CounterOpts{
	Namespace: connection.MetricsNamespace,
	Subsystem: "bootstrap",
	Name:      "attempts_total",
	Help:      "Number of bootstrap fetch-and-validate attempts by outcome.",
}, []string{"outcome"})

type attemptError struct {
	outcome string
	err     error
}

func (e *attemptError) Error() string { return e.err.Error() }
func (e *attemptError) Unwrap() error { return e.err }

func bootstrapErrorf(outcome, format string, args ...any) error {
	return &attemptError{outcome: outcome, err: fmt.Errorf(format, args...)}
}

func httpOutcome(status int) string {
	switch {
	case status >= http.StatusBadRequest && status < http.StatusInternalServerError:
		return outcomeHTTP4xx
	case status >= http.StatusInternalServerError && status < 600:
		return outcomeHTTP5xx
	default:
		return outcomeHTTPOther
	}
}

// recordAttempt counts one completed fetch-and-validate attempt.
func recordAttempt(ctx context.Context, err error) {
	outcome := outcomeOtherError
	parentErr := ctx.Err()
	if err == nil {
		outcome = outcomeSuccess
	} else if parentErr != nil && errors.Is(err, parentErr) {
		// Count cancellation only when the attempt failed with the parent context's error.
		outcome = outcomeCanceled
	} else if failure, ok := errors.AsType[*attemptError](err); ok {
		outcome = failure.outcome
	}
	attempts.WithLabelValues(outcome).Inc()
}
