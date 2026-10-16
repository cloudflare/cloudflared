package ingress

import (
	"context"
	"net"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDialOriginRetryReturnsPermanentErrors(t *testing.T) {
	t.Parallel()
	// Injecting OS errors avoids platform-specific permissions and DNS dependencies.
	testCases := []struct {
		name             string
		network          string
		err              error
		initiallyRefused bool
	}{
		{"permission denied", "unix", syscall.EACCES, false},
		{"not a directory", "unix", syscall.ENOTDIR, false},
		{"invalid address", "unix", syscall.EINVAL, false},
		{"network unreachable", "tcp", syscall.ENETUNREACH, false},
		{"DNS failure", "tcp", &net.DNSError{Err: "no such host", Name: "origin", IsNotFound: true}, false},
		{"dial timeout", "tcp", context.DeadlineExceeded, false},
		{"permanent failure after refused connection", "unix", syscall.EACCES, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				attempts := 0
				dial := func(context.Context, string, string) (net.Conn, error) {
					attempts++
					if tc.initiallyRefused && attempts == 1 {
						return nil, &net.OpError{Op: "dial", Net: tc.network, Err: errOriginConnectionRefused}
					}
					return nil, &net.OpError{Op: "dial", Net: tc.network, Err: tc.err}
				}
				conn, err := dialOriginWithRetry(t.Context(), tc.network, "origin", time.Second, dial)
				require.Nil(t, conn)
				require.ErrorIs(t, err, tc.err)
				expectedAttempts := 1
				if tc.initiallyRefused {
					expectedAttempts++
				}
				require.Equal(t, expectedAttempts, attempts, "permanent failures must not trigger another dial")
			})
		})
	}
}

func TestDialOriginRetryBoundsConnectionTime(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		retryTimeout   time.Duration
		requestTimeout time.Duration
		firstRefusal   time.Duration
	}{
		{"retry timeout during first dial", 50 * time.Millisecond, time.Second, 0},
		{"retry timeout includes previous dials", 50 * time.Millisecond, time.Second, 25 * time.Millisecond},
		{"shorter request deadline", time.Second, 50 * time.Millisecond, 0},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				refused := false
				dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
					if tc.firstRefusal > 0 && !refused {
						refused = true
						time.Sleep(tc.firstRefusal)
						return nil, errOriginConnectionRefused
					}
					<-ctx.Done()
					return nil, ctx.Err()
				}
				ctx, cancel := context.WithTimeout(t.Context(), tc.requestTimeout)
				defer cancel()
				start := time.Now()
				conn, err := dialOriginWithRetry(ctx, "tcp", "origin", tc.retryTimeout, dial)
				elapsed := time.Since(start)
				require.Nil(t, conn)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Equal(t, 50*time.Millisecond, elapsed)
			})
		})
	}
}
