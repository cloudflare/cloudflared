package supervisor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/retry"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

func immediateTimeAfter(time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	c <- time.Now()
	return c
}

func TestWaitForBackoffFallback(t *testing.T) {
	maxRetries := uint(3)
	backoff := retry.NewBackoff(maxRetries, 40*time.Millisecond, false)
	backoff.Clock.After = immediateTimeAfter
	log := zerolog.Nop()
	protocolSelector, err := connection.NewProtocolSelector("auto", &log)
	require.NoError(t, err)

	initProtocol := protocolSelector.Current()
	assert.Equal(t, connection.QUIC, initProtocol)

	protoFallback := &protocolFallback{
		backoff,
		initProtocol,
		false,
	}

	// Retry #0 and #1. At retry #2, we switch protocol, so the fallback loop has one more retry than this
	for i := 0; i < int(maxRetries-1); i++ {
		protoFallback.BackoffTimer() // simulate retry
		ok := selectNextProtocol(&log, protoFallback, protocolSelector, nil)
		assert.True(t, ok)
		assert.Equal(t, initProtocol, protoFallback.protocol)
	}

	// Retry fallback protocol
	protoFallback.BackoffTimer() // simulate retry
	ok := selectNextProtocol(&log, protoFallback, protocolSelector, nil)
	assert.True(t, ok)
	fallback, ok := protocolSelector.Fallback()
	assert.True(t, ok)
	assert.Equal(t, fallback, protoFallback.protocol)
	assert.Equal(t, connection.HTTP2, protoFallback.protocol)

	currentGlobalProtocol := protocolSelector.Current()
	assert.Equal(t, initProtocol, currentGlobalProtocol)

	// Simulate max retries again (retries reset after protocol switch)
	for i := 0; i < int(maxRetries); i++ {
		protoFallback.BackoffTimer()
	}
	// No protocol to fallback, return error
	ok = selectNextProtocol(&log, protoFallback, protocolSelector, nil)
	assert.False(t, ok)

	protoFallback.reset()
	protoFallback.BackoffTimer() // simulate retry
	ok = selectNextProtocol(&log, protoFallback, protocolSelector, nil)
	assert.True(t, ok)
	assert.Equal(t, initProtocol, protoFallback.protocol)

	protoFallback.reset()
	protoFallback.BackoffTimer() // simulate retry
	ok = selectNextProtocol(&log, protoFallback, protocolSelector, &quic.IdleTimeoutError{})
	// Check that we get a true after the first try itself when this flag is true. This allows us to immediately
	// switch protocols when there is a fallback.
	assert.True(t, ok)

	// But if there is no fallback available, then we exhaust the retries despite the type of error.
	// The reason why there's no fallback available is because we pick a specific protocol instead of letting it be auto.
	protocolSelector, err = connection.NewProtocolSelector("quic", &log)
	require.NoError(t, err)
	protoFallback = &protocolFallback{backoff, protocolSelector.Current(), false}
	for i := 0; i < int(maxRetries-1); i++ {
		protoFallback.BackoffTimer() // simulate retry
		ok := selectNextProtocol(&log, protoFallback, protocolSelector, &quic.IdleTimeoutError{})
		assert.True(t, ok)
		assert.Equal(t, connection.QUIC, protoFallback.protocol)
	}
	// And finally it fails as it should, with no fallback.
	protoFallback.BackoffTimer()
	ok = selectNextProtocol(&log, protoFallback, protocolSelector, &quic.IdleTimeoutError{})
	assert.False(t, ok)
}

func TestIsRetryableStartupError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "duplicate connection",
			err:       connection.ErrDuplicateConnection,
			retryable: true,
		},
		{
			name:      "quic idle timeout",
			err:       &quic.IdleTimeoutError{},
			retryable: true,
		},
		{
			name:      "edge quic dial error",
			err:       &connection.EdgeQuicDialError{Cause: errors.New("dial failed")},
			retryable: true,
		},
		{
			name:      "control stream error",
			err:       &connection.ControlStreamError{Cause: errors.New("control failed")},
			retryable: true,
		},
		{
			name:      "stream listener error",
			err:       &connection.StreamListenerError{Cause: errors.New("accept failed")},
			retryable: true,
		},
		{
			name:      "datagram manager error",
			err:       &connection.DatagramManagerError{Cause: errors.New("datagram failed")},
			retryable: true,
		},
		{
			name:      "datagram manager error wrapping quic application error",
			err:       &connection.DatagramManagerError{Cause: &quic.ApplicationError{}},
			retryable: true,
		},
		{
			name:      "further wrapped connection error",
			err:       fmt.Errorf("serve tunnel: %w", &connection.ControlStreamError{Cause: errors.New("boom")}),
			retryable: true,
		},
		{
			name:      "unknown error bails startup",
			err:       errors.New("some unexpected error"),
			retryable: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.retryable, isRetryableStartupError(tc.err))
		})
	}
}

func TestShouldGetNewAddress(t *testing.T) {
	t.Parallel()

	registrationCause := errors.New("Unauthorized: Invalid tunnel secret")

	tests := []struct {
		name             string
		err              error
		wantNewAddress   bool
		wantConnectivity bool
	}{
		{
			name:           "nil keeps address",
			err:            nil,
			wantNewAddress: false,
		},
		{
			name:           "dup conn rotates address",
			err:            connection.ErrDuplicateConnection,
			wantNewAddress: true,
		},
		{
			name:           "dup conn wrapped in control stream error rotates address",
			err:            &connection.ControlStreamError{Cause: connection.ErrDuplicateConnection},
			wantNewAddress: true,
		},
		{
			name:           "quic idle timeout rotates address",
			err:            &quic.IdleTimeoutError{},
			wantNewAddress: true,
		},
		{
			name:           "quic idle timeout wrapped rotates address",
			err:            &connection.ControlStreamError{Cause: &quic.IdleTimeoutError{}},
			wantNewAddress: true,
		},
		{
			name:             "edge quic dial error is a connectivity error",
			err:              &connection.EdgeQuicDialError{Cause: errors.New("dial failed")},
			wantNewAddress:   true,
			wantConnectivity: true,
		},
		{
			name:             "edge quic dial error wrapping idle timeout is a connectivity error",
			err:              &connection.EdgeQuicDialError{Cause: &quic.IdleTimeoutError{}},
			wantNewAddress:   true,
			wantConnectivity: true,
		},
		{
			name:           "server registration error keeps address",
			err:            connection.ServerRegisterTunnelError{Cause: registrationCause, Permanent: true},
			wantNewAddress: false,
		},
		{
			name:           "unknown error keeps address",
			err:            errors.New("some unexpected error"),
			wantNewAddress: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fallback := NewIPAddrFallback(1)
			needsNewAddress, connectivityErr := fallback.ShouldGetNewAddress(0, tc.err)
			assert.Equal(t, tc.wantNewAddress, needsNewAddress)
			if tc.wantConnectivity {
				assert.Error(t, connectivityErr)
			} else {
				assert.NoError(t, connectivityErr)
			}
		})
	}
}

func TestShouldGetNewAddressCountsWrappedQUICDialTimeout(t *testing.T) {
	t.Parallel()

	fallback := NewIPAddrFallback(1)
	dialErr := &connection.EdgeQuicDialError{Cause: &quic.IdleTimeoutError{}}

	needsNewAddress, err := fallback.ShouldGetNewAddress(0, dialErr)
	require.True(t, needsNewAddress)
	connectivityErr, ok := errors.AsType[*ConnectivityError](err)
	require.True(t, ok)
	assert.False(t, connectivityErr.HasReachedMaxRetries())

	needsNewAddress, err = fallback.ShouldGetNewAddress(0, dialErr)
	require.True(t, needsNewAddress)
	connectivityErr, ok = errors.AsType[*ConnectivityError](err)
	require.True(t, ok)
	assert.True(t, connectivityErr.HasReachedMaxRetries())
}

// scriptedTunnelServer is a fake TunnelServer that returns a predefined sequence of
// errors, recording how many times Serve was called. When the script is exhausted it
// cancels the provided context to unblock the retry loop.
type scriptedTunnelServer struct {
	errs   []error
	calls  int
	cancel context.CancelFunc
}

func (s *scriptedTunnelServer) Serve(_ context.Context, _ uint8, _ *protocolFallback, connectedSignal *signal.Signal) error {
	defer func() { s.calls++ }()
	if s.calls >= len(s.errs) {
		// Script exhausted: stop the loop by cancelling the supervisor context.
		s.cancel()
		return context.Canceled
	}
	return s.errs[s.calls]
}

func newTestSupervisor(t *testing.T, server TunnelServer) *Supervisor {
	t.Helper()
	log := zerolog.Nop()
	observer := connection.NewObserver(&log, &log)
	tracker := tunnelstate.NewConnTracker(&log)
	connAwareLogger := NewConnAwareLogger(&log, tracker, observer)

	backoff := retry.NewBackoff(1, time.Millisecond, true /* retryForever */)
	backoff.Clock.After = immediateTimeAfter

	return &Supervisor{
		config:           &TunnelConfig{},
		edgeTunnelServer: server,
		tunnelErrors:     make(chan tunnelError, 1),
		tunnelsProtocolFallback: map[int]*protocolFallback{
			0: {backoff, connection.QUIC, false},
		},
		log: connAwareLogger,
	}
}

func TestStartFirstTunnelRetryLoop(t *testing.T) {
	t.Parallel()

	dupConn := &connection.ControlStreamError{Cause: connection.ErrDuplicateConnection}
	retryableRegistration := &connection.ControlStreamError{Cause: connection.ServerRegisterTunnelError{
		Cause:     errors.New("retry registration"),
		Permanent: false,
	}}
	permanentRegistration := &connection.ControlStreamError{Cause: connection.ServerRegisterTunnelError{
		Cause:     errors.New("invalid registration"),
		Permanent: true,
	}}

	tests := []struct {
		name          string
		errs          []error
		wantMinCalls  int
		wantExactCall int // 0 means "at least wantMinCalls"
	}{
		{
			name:          "retryable error keeps looping",
			errs:          []error{dupConn, dupConn},
			wantMinCalls:  3, // 2 scripted retryable errors + final exhausted call
			wantExactCall: 3,
		},
		{
			name:          "retryable registration error keeps looping",
			errs:          []error{retryableRegistration},
			wantMinCalls:  2,
			wantExactCall: 2,
		},
		{
			name:          "permanent registration error exits immediately",
			errs:          []error{permanentRegistration},
			wantMinCalls:  1,
			wantExactCall: 1,
		},
		{
			name:          "unauthorized registration error keeps looping",
			errs:          []error{connection.ServerRegisterTunnelError{Cause: errors.New("Unauthorized: bad secret"), Permanent: true}},
			wantMinCalls:  2, // 1 scripted transient error + final exhausted call
			wantExactCall: 2,
		},
		{
			name:          "non retryable error exits immediately",
			errs:          []error{errors.New("some unexpected fatal error")},
			wantMinCalls:  1,
			wantExactCall: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			server := &scriptedTunnelServer{errs: tc.errs, cancel: cancel}
			s := newTestSupervisor(t, server)

			connectedSignal := signal.New(make(chan struct{}))

			done := make(chan struct{})
			go func() {
				s.startFirstTunnel(ctx, connectedSignal)
				close(done)
			}()

			// startFirstTunnel delivers the final error on tunnelErrors before returning.
			select {
			case <-s.tunnelErrors:
			case <-time.After(5 * time.Second):
				t.Fatal("startFirstTunnel did not finish in time")
			}
			<-done

			if tc.wantExactCall > 0 {
				assert.Equal(t, tc.wantExactCall, server.calls)
			} else {
				assert.GreaterOrEqual(t, server.calls, tc.wantMinCalls)
			}
		})
	}
}
