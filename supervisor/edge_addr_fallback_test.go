package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	cfdquic "github.com/cloudflare/cloudflared/quic"
	"github.com/cloudflare/cloudflared/retry"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// Reproduce an edge that repeatedly registers a connection, then fails its control
// stream. Use the real QUIC serving and address-selection code, injecting only
// the transport and control-stream handler so no external edge fault is needed.
func TestControlStreamFailureRotatesEdgeAddress(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	log := zerolog.Nop()
	edgeAddrs, err := edgediscovery.StaticEdge(&log, []string{"127.0.0.1:7844", "127.0.0.2:7844"})
	require.NoError(t, err)
	fallback := NewIPAddrFallback(8)
	backoff := &protocolFallback{
		BackoffHandler: retry.NewBackoff(5, time.Millisecond, true),
		protocol:       connection.QUIC,
	}
	backoff.Clock.After = immediateTimeAfter
	cause := errors.New("control stream encountered a failure while serving")
	seen := make(map[string]bool)

	for attempt := range 12 {
		addr, err := edgeAddrs.GetAddr(0)
		require.NoError(t, err)
		seen[addr.UDP.String()] = true
		fuse := &connectedFuse{fuse: newBooleanFuse(), backoff: backoff}
		tunnel := connection.NewTunnelConnection(
			ctx, &controlFailureQUICConn{}, 0, nil, waitingDatagramHandler{},
			failingControlStream{connected: fuse, err: cause}, &client.ConnectionOptionsSnapshot{},
			time.Second, time.Second, 0, &log,
		)
		err = tunnel.Serve(ctx)
		require.NoError(t, ctx.Err())
		var controlErr *connection.ControlStreamError
		require.ErrorAs(t, err, &controlErr)
		require.ErrorIs(t, err, cause)
		require.True(t, fuse.IsConnected())
		require.Zero(t, backoff.Retries(), "registration resets the retry backoff")

		// These are the same policy and pool operations performed by EdgeTunnelServer.Serve.
		rotate, connectivityErr := fallback.ShouldGetNewAddress(0, err)
		require.NoError(t, connectivityErr)
		if rotate {
			_, err = edgeAddrs.GetDifferentAddr(0, true)
			require.NoError(t, err)
		}
		<-backoff.BackoffTimer()
		t.Logf("attempt %d: address=%s rotate=%t", attempt+1, addr.UDP, rotate)
	}
	assert.Greater(t, len(seen), 1, "control-stream failures must not pin retries to one edge address")
}

func TestShouldGetNewAddressForControlStreamErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		wantNewAddress bool
	}{
		{
			name:           "control stream failure",
			err:            &connection.ControlStreamError{Cause: errors.New("control stream failed")},
			wantNewAddress: true,
		},
		{
			name:           "wrapped control stream failure",
			err:            fmt.Errorf("serve tunnel: %w", &connection.ControlStreamError{Cause: io.EOF}),
			wantNewAddress: true,
		},
		{
			name:           "control stream deadline",
			err:            &connection.ControlStreamError{Cause: context.DeadlineExceeded},
			wantNewAddress: true,
		},
		{
			name:           "control stream closed by edge",
			err:            &connection.ControlStreamError{Cause: &quic.ApplicationError{}},
			wantNewAddress: true,
		},
		{
			name: "retryable registration failure",
			err: &connection.ControlStreamError{Cause: connection.ServerRegisterTunnelError{
				Cause: errors.New("edge temporarily unavailable"),
			}},
			wantNewAddress: true,
		},
		{
			name: "permanent registration failure keeps address",
			err: &connection.ControlStreamError{Cause: connection.ServerRegisterTunnelError{
				Cause:     errors.New("Unauthorized: Invalid tunnel secret"),
				Permanent: true,
			}},
		},
		{
			name: "wrapped permanent registration failure keeps address",
			err: fmt.Errorf("serve tunnel: %w", &connection.ControlStreamError{Cause: fmt.Errorf("registration: %w", connection.ServerRegisterTunnelError{
				Cause:     errors.New("invalid registration"),
				Permanent: true,
			})}),
		},
		{
			name: "duplicate connection retains precedence",
			err: &connection.ControlStreamError{Cause: connection.ServerRegisterTunnelError{
				Cause:     connection.ErrDuplicateConnection,
				Permanent: true,
			}},
			wantNewAddress: true,
		},
		{
			name:           "graceful completion keeps address",
			err:            nil,
			wantNewAddress: false,
		},
		{
			name: "other application error keeps address",
			err:  errors.New("unrelated failure"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fallback := NewIPAddrFallback(8)
			for range 12 {
				rotate, connectivityErr := fallback.ShouldGetNewAddress(0, tc.err)
				assert.Equal(t, tc.wantNewAddress, rotate)
				require.NoError(t, connectivityErr)
			}
		})
	}
}

func TestControlStreamFailurePreservesDialRetryBudget(t *testing.T) {
	t.Parallel()

	fallback := NewIPAddrFallback(1)
	dialErr := &connection.EdgeQuicDialError{Cause: errors.New("dial failed")}
	rotate, err := fallback.ShouldGetNewAddress(0, dialErr)
	require.True(t, rotate)
	var connectivityErr *ConnectivityError
	require.ErrorAs(t, err, &connectivityErr)
	require.False(t, connectivityErr.HasReachedMaxRetries())

	for range 12 {
		rotate, err = fallback.ShouldGetNewAddress(0, &connection.ControlStreamError{Cause: io.EOF})
		require.True(t, rotate)
		require.NoError(t, err)
	}

	// Control-stream failures neither exhaust nor reset the dial failure budget.
	rotate, err = fallback.ShouldGetNewAddress(0, dialErr)
	require.True(t, rotate)
	require.ErrorAs(t, err, &connectivityErr)
	assert.True(t, connectivityErr.HasReachedMaxRetries())

	// Another connection index keeps its own dial failure budget.
	rotate, err = fallback.ShouldGetNewAddress(1, dialErr)
	require.True(t, rotate)
	require.ErrorAs(t, err, &connectivityErr)
	assert.False(t, connectivityErr.HasReachedMaxRetries())
}

type controlFailureQUICConn struct {
	cfdquic.QUICConnection
}

func (*controlFailureQUICConn) OpenStream() (*quic.Stream, error) {
	return nil, nil
}

func (*controlFailureQUICConn) AcceptStream(ctx context.Context) (*quic.Stream, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*controlFailureQUICConn) CloseWithError(quic.ApplicationErrorCode, string) error {
	return nil
}

type failingControlStream struct {
	connected connection.ConnectedFuse
	err       error
}

func (f failingControlStream) ServeControlStream(context.Context, io.ReadWriteCloser, *pogs.ConnectionOptions, connection.TunnelConfigJSONGetter) error {
	f.connected.Connected()
	return f.err
}

func (failingControlStream) IsStopped() bool {
	return false
}

type waitingDatagramHandler struct {
	pogs.SessionManager
}

func (waitingDatagramHandler) Serve(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}
