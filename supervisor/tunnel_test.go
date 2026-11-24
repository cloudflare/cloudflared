package supervisor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/edgediscovery"
	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/retry"
	"github.com/cloudflare/cloudflared/signal"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

type tunnelTestFeatures struct {
	featuresList []string
}

func (f tunnelTestFeatures) Snapshot() features.FeatureSnapshot {
	return features.FeatureSnapshot{DatagramVersion: features.DatagramV3, FeaturesList: f.featuresList}
}

type tunnelTestRegistrationServer struct{}

func (tunnelTestRegistrationServer) RegisterConnection(context.Context, pogs.TunnelAuth, uuid.UUID, byte, *pogs.ConnectionOptions) (*pogs.ConnectionDetails, error) {
	return &pogs.ConnectionDetails{UUID: uuid.New(), Location: "test", TunnelIsRemotelyManaged: true}, nil
}

func (tunnelTestRegistrationServer) UnregisterConnection(context.Context) {}

func (tunnelTestRegistrationServer) UpdateLocalConfiguration(context.Context, []byte) error {
	return nil
}

func TestMapQUICCloseError(t *testing.T) {
	t.Parallel()

	remoteRegionChange := &quic.ApplicationError{Remote: true, ErrorCode: connection.RegionChangeQUICCode}
	localRegionChange := &quic.ApplicationError{ErrorCode: connection.RegionChangeQUICCode}
	unknownClose := &quic.ApplicationError{Remote: true, ErrorCode: connection.RegionChangeQUICCode + 1}
	wrappedUnknownClose := fmt.Errorf("connection closed: %w", unknownClose)
	otherError := errors.New("unexpected failure")
	wrappedOtherError := fmt.Errorf("connection failed: %w", otherError)

	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"remote region change", remoteRegionChange, connection.ErrRegionChanged},
		{"wrapped remote region change", fmt.Errorf("connection closed: %w", remoteRegionChange), connection.ErrRegionChanged},
		{"local region change", localRegionChange, localRegionChange},
		{"unknown remote close", unknownClose, unknownClose},
		{"wrapped unknown remote close", wrappedUnknownClose, wrappedUnknownClose},
		{"other error", otherError, otherError},
		{"wrapped other error", wrappedOtherError, wrappedOtherError},
		{"cancellation", context.Canceled, context.Canceled},
		{"nil", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := mapQUICCloseError(tc.err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestQUICCloseErrorPreservesUnknownCodes(t *testing.T) {
	t.Parallel()

	for _, code := range []quic.ApplicationErrorCode{0, 1, connection.RegionChangeQUICCode + 1, 1<<62 - 1} {
		for _, remote := range []bool{false, true} {
			t.Run(fmt.Sprintf("code=%d/remote=%t", code, remote), func(t *testing.T) {
				t.Parallel()

				err := &quic.ApplicationError{Remote: remote, ErrorCode: code}
				require.Same(t, err, mapQUICCloseError(err), "unrecognized codes must preserve the original error")

				wrapped := fmt.Errorf("connection closed: %w", err)
				require.Same(t, wrapped, mapQUICCloseError(wrapped), "unrecognized codes must preserve the original wrappers")
			})
		}
	}
}

func TestServeQUICRegionChange(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		code        quic.ApplicationErrorCode
		features    []string
		wantRestart bool
	}{
		{"opted in", connection.RegionChangeQUICCode, []string{features.FeatureRegionalization}, true},
		{"disabled by default", connection.RegionChangeQUICCode, nil, false},
		{"unrelated close", connection.RegionChangeQUICCode + 1, []string{features.FeatureRegionalization}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			// Use a local QUIC peer that completes registration, then closes with
			// either the region-change code or an unrelated application error.
			listener, tlsConfig := newTestQUICRegionChangePeer(t)
			connected := signal.New(make(chan struct{}))
			edgeDone := make(chan error, 1)
			go func() {
				edgeDone <- serveTestQUICRegionChangePeer(ctx, listener, tc.code, connected)
			}()
			defer func() {
				cancel()
				require.NoError(t, <-edgeDone)
			}()

			// Disable retries so the result reflects this connection's close code.
			server := newTestQUICEdgeTunnelServer(t, listener.Addr().String(), tlsConfig, tc.features)

			// Expected restarts should not produce the failure logs used for other close codes.
			var failureLogs atomic.Int32
			log := zerolog.New(io.Discard).Hook(zerolog.HookFunc(func(_ *zerolog.Event, level zerolog.Level, _ string) {
				if level == zerolog.WarnLevel || level == zerolog.ErrorLevel {
					failureLogs.Add(1)
				}
			}))
			server.config.Log = &log
			server.connAwareLogger = server.connAwareLogger.ReplaceLogger(&log)

			fallback := &protocolFallback{retry.NewBackoff(0, time.Millisecond, false), connection.QUIC, false}
			err := server.Serve(ctx, 0, fallback, connected)

			// Only the designated code should trigger a group restart. Preserve
			// unrelated peer errors so ordinary retry and fallback still apply.
			if tc.wantRestart {
				require.ErrorIs(t, err, connection.ErrRegionChanged)
				require.Zero(t, failureLogs.Load(), "a requested region change must not be logged as a connection failure")
			} else {
				require.NotErrorIs(t, err, connection.ErrRegionChanged)
				appErr, ok := errors.AsType[*quic.ApplicationError](err)
				require.True(t, ok, "expected wrapped application error, got %v", err)
				require.True(t, appErr.Remote)
				require.Equal(t, tc.code, appErr.ErrorCode)
				require.Positive(t, failureLogs.Load(), "ordinary connection failures must still be logged")
			}
		})
	}
}

func newTestQUICRegionChangePeer(t *testing.T) (*quic.Listener, *tls.Config) {
	t.Helper()

	certServer := httptest.NewTLSServer(nil)
	t.Cleanup(certServer.Close)

	listener, err := quic.ListenAddr("127.0.0.1:0", &tls.Config{
		Certificates: certServer.TLS.Certificates,
		NextProtos:   []string{"argotunnel"},
	}, &quic.Config{EnableDatagrams: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	// Trust only the local peer's certificate, without disabling TLS verification.
	roots := x509.NewCertPool()
	roots.AddCert(certServer.Certificate())
	return listener, &tls.Config{RootCAs: roots, ServerName: "example.com", NextProtos: []string{"argotunnel"}}
}

func serveTestQUICRegionChangePeer(ctx context.Context, listener *quic.Listener, code quic.ApplicationErrorCode, connected *signal.Signal) error {
	conn, err := listener.Accept(ctx)
	if err != nil {
		return err
	}
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return err
	}

	rpcDone := make(chan error, 1)
	go func() {
		rpcDone <- tunnelrpc.NewRegistrationServer(tunnelTestRegistrationServer{}).Serve(ctx, stream)
	}()

	// Wait until cloudflared has received the registration response. Closing
	// earlier tests an interrupted registration rather than a region change.
	select {
	case <-connected.Wait():
		err = conn.CloseWithError(code, "test close")
	case <-ctx.Done():
		err = ctx.Err()
	}

	// Closing the connection also ends the registration RPC server. Ignore its
	// expected shutdown error, but propagate errors while the connection is live.
	if rpcErr := <-rpcDone; rpcErr != nil && conn.Context().Err() == nil && err == nil {
		err = rpcErr
	}
	return err
}

func newTestQUICEdgeTunnelServer(t *testing.T, address string, tlsConfig *tls.Config, featureList []string) *EdgeTunnelServer {
	t.Helper()

	log := zerolog.Nop()
	observer := connection.NewObserver(&log)
	tracker := tunnelstate.NewConnTracker(&log)
	addrs, err := edgediscovery.StaticEdge(&log, []string{address})
	require.NoError(t, err)

	// Keep the loopback address in IPv4 form so Darwin selects a UDPv4 socket.
	addr, err := addrs.GetAddr(0)
	require.NoError(t, err)
	addr.UDP.IP = addr.UDP.IP.To4()

	clientConfig, err := client.NewConfig("test", "test", tunnelTestFeatures{featuresList: featureList})
	require.NoError(t, err)

	return &EdgeTunnelServer{
		config: &TunnelConfig{
			Log: &log, Observer: observer, ClientConfig: clientConfig,
			NamedTunnel: &connection.TunnelProperties{},
			RPCTimeout:  3 * time.Second,
			EdgeTLSConfigs: map[connection.Protocol]*tls.Config{
				connection.QUIC: tlsConfig,
			},
		},
		edgeAddrs:       addrs,
		edgeAddrHandler: NewIPAddrFallback(1),
		connAwareLogger: NewConnAwareLogger(&log, tracker, observer),
	}
}

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
	observer := connection.NewObserver(&log)
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
