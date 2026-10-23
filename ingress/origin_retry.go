package ingress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/cloudflare/cloudflared/retry"
)

// HTTP transport with request-scoped cancellation for optional connection retries.
type originHTTPTransport struct {
	*http.Transport
	connectRetryTimeout time.Duration
}

type originDialContextKey struct{}

func (t *originHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.connectRetryTimeout <= 0 {
		return t.Transport.RoundTrip(req)
	}

	// Preserving cancellation across net/http's detached dial context; a request
	// that finishes using a pooled connection must also stop its pending retries.
	dialCtx, cancel := context.WithCancel(req.Context())
	defer cancel()
	ctx := context.WithValue(req.Context(), originDialContextKey{}, dialCtx)
	return t.Transport.RoundTrip(req.WithContext(ctx))
}

// Dial the origin within a bounded retry window, before any request is written.
// Only absent listeners are retried; TLS and HTTP errors remain the transport's responsibility.
func dialOriginWithRetry(
	ctx context.Context,
	network, address string,
	timeout time.Duration,
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	if timeout <= 0 {
		return dial(ctx, network, address)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if requestCtx, ok := ctx.Value(originDialContextKey{}).(context.Context); ok {
		stop := context.AfterFunc(requestCtx, cancel)
		defer stop()
		err := requestCtx.Err()
		if err != nil {
			return nil, err
		}
	}

	// Capping jittered exponential backoff at 80ms to cover short local restarts.
	backoff := retry.NewBackoff(3, 10*time.Millisecond, true)
	for {
		conn, err := dial(ctx, network, address)
		if err == nil {
			return conn, nil
		}
		if !errors.Is(err, errOriginConnectionRefused) && (network != "unix" || !errors.Is(err, syscall.ENOENT)) {
			return nil, err
		}
		again := backoff.Backoff(ctx)
		if !again {
			return nil, fmt.Errorf("origin connection retry stopped: %w (last dial error: %w)", ctx.Err(), err)
		}
	}
}
