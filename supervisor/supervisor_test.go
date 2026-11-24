package supervisor

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/bootstrap"
	"github.com/cloudflare/cloudflared/client"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/signal"
)

type bootstrapFetcherFunc func(context.Context, connection.Credentials, *client.Config) (*bootstrap.Response, error)

func (f bootstrapFetcherFunc) FetchMetadata(ctx context.Context, creds connection.Credentials, config *client.Config) (*bootstrap.Response, error) {
	return f(ctx, creds, config)
}

type regionChangeTunnelServer func(context.Context, uint8, *signal.Signal) error

func (f regionChangeTunnelServer) Serve(ctx context.Context, index uint8, _ *protocolFallback, connected *signal.Signal) error {
	return f(ctx, index, connected)
}

func newRegionChangeTunnelServer(t *testing.T, ctx context.Context, cancel context.CancelFunc, haConnections int32, notification error) (regionChangeTunnelServer, *atomic.Int32, *atomic.Int32) {
	t.Helper()

	var calls, oldActive atomic.Int32
	siblingReady := make(chan struct{})
	server := regionChangeTunnelServer(func(groupCtx context.Context, index uint8, connected *signal.Signal) error {
		call := calls.Add(1)

		// The first HA group must fully connect before reporting the region
		// change. Its other connection waits for the supervisor to cancel it.
		if call <= haConnections {
			oldActive.Add(1)
			defer oldActive.Add(-1)

			connected.Notify()
			if int32(index) == haConnections-1 {
				close(siblingReady)
				<-groupCtx.Done()
				if err := ctx.Err(); err != nil {
					t.Errorf("sibling must stop before the test context is cancelled: %v", err)
				}
				return groupCtx.Err()
			}

			select {
			case <-siblingReady:
				return notification
			case <-groupCtx.Done():
				return groupCtx.Err()
			}
		}

		// The replacement group may start only after every old connection
		// has exited. Stop the test once all replacements are connected.
		assert.Zero(t, oldActive.Load(), "new connections must follow complete old-group teardown")
		connected.Notify()
		if call == 2*haConnections {
			cancel()
		}

		<-groupCtx.Done()
		return groupCtx.Err()
	})
	return server, &calls, &oldActive
}

func setBootstrapFetcher(t *testing.T, s *Supervisor, fetch bootstrapFetcherFunc) {
	t.Helper()
	s.config.NamedTunnel = &connection.TunnelProperties{Credentials: connection.Credentials{
		AccountTag: "account", TunnelID: uuid.New(), TunnelSecret: []byte("secret"),
	}}
	s.config.ClientConfig = &client.Config{Version: "test", Arch: "darwin_arm64", ConnectorID: uuid.New()}
	s.bootstrapClient = fetch
}

func TestRegionChangeDrainsHAAndRefreshesOnce(t *testing.T) {
	t.Parallel()

	const haConnections = 2

	for _, notification := range []struct {
		name string
		err  error
	}{
		{"sentinel", connection.ErrRegionChanged},
		{"wrapped sentinel", fmt.Errorf("connection terminated: %w", connection.ErrRegionChanged)},
	} {
		t.Run(notification.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			base := &tls.Config{ServerName: "old.example"}
			server, calls, oldActive := newRegionChangeTunnelServer(t, ctx, cancel, haConnections, notification.err)

			s := newTestSupervisor(t, server)
			s.tunnelsConnecting = make(map[int]chan struct{})
			s.config.HAConnections = haConnections
			s.config.EdgeTLSConfigs = map[connection.Protocol]*tls.Config{connection.QUIC: base, connection.HTTP2: base}

			selector, err := connection.NewProtocolSelector("quic", s.log.Logger())
			require.NoError(t, err)
			s.config.ProtocolSelector = selector

			// Bootstrap must run once, after the original group has drained and
			// before either replacement connection starts.
			var refreshes atomic.Int32
			setBootstrapFetcher(t, s, func(_ context.Context, creds connection.Credentials, _ *client.Config) (*bootstrap.Response, error) {
				assert.Zero(t, oldActive.Load(), "bootstrap must follow complete old-group teardown")
				assert.Equal(t, int32(haConnections), calls.Load(), "no new connection may start before bootstrap")
				refreshes.Add(1)
				return &bootstrap.Response{
					AccountTag: creds.AccountTag,
					TunnelID:   creds.TunnelID,
					SNI:        map[string]string{"quic": "ch.example", "http2": "ch.example"},
				}, nil
			})

			require.NoError(t, s.runWithRegionChanges(ctx, signal.New(make(chan struct{}))))
			require.Equal(t, int32(1), refreshes.Load())
			require.Equal(t, int32(2*haConnections), calls.Load(), "expected the original and replacement HA groups")
			require.Equal(t, "ch.example", s.config.EdgeTLSConfigs[connection.QUIC].ServerName)
			require.Equal(t, "old.example", s.config.EdgeTLSConfigs[connection.HTTP2].ServerName)
			require.NotSame(t, base, s.config.EdgeTLSConfigs[connection.QUIC])
			require.Same(t, base, s.config.EdgeTLSConfigs[connection.HTTP2])
			require.Equal(t, "old.example", base.ServerName)
		})
	}
}

func TestRegionChangeShutdownInterruptsBootstrap(t *testing.T) {
	// The restart counter is shared, so check its delta serially.
	for _, graceful := range []bool{false, true} {
		t.Run(fmt.Sprint(graceful), func(t *testing.T) {
			restartsBefore := regionChangeRestartCount(t)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			shutdown := make(chan struct{})
			var starts atomic.Int32
			server := regionChangeTunnelServer(func(_ context.Context, _ uint8, connected *signal.Signal) error {
				starts.Add(1)
				connected.Notify()
				return connection.ErrRegionChanged
			})
			s := newTestSupervisor(t, server)
			s.config.HAConnections = 1
			selector, err := connection.NewProtocolSelector("quic", s.log.Logger())
			require.NoError(t, err)
			s.config.ProtocolSelector = selector
			s.gracefulShutdownC = shutdown
			var requested bool
			setBootstrapFetcher(t, s, func(fetchCtx context.Context, _ connection.Credentials, _ *client.Config) (*bootstrap.Response, error) {
				requested = true
				if graceful {
					close(shutdown)
				} else {
					cancel()
				}
				<-fetchCtx.Done()
				return nil, fetchCtx.Err()
			})
			require.NoError(t, s.runWithRegionChanges(ctx, signal.New(make(chan struct{}))))
			require.True(t, requested, "shutdown must interrupt an active bootstrap request")
			require.Equal(t, int32(1), starts.Load(), "shutdown must prevent starting a replacement group")
			require.InDelta(t, restartsBefore, regionChangeRestartCount(t), 0)
		})
	}
}

func TestRegionChangeRestartInterval(t *testing.T) {
	// The restart counter is shared, so check its delta serially.
	restartsBefore := regionChangeRestartCount(t)

	s := newTestSupervisor(t, nil)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 2*tunnelRetryDuration)
		defer cancel()
		s.config.NamedTunnel = &connection.TunnelProperties{}
		s.config.HAConnections = 1
		s.bootstrapClient = bootstrapFetcherFunc(func(context.Context, connection.Credentials, *client.Config) (*bootstrap.Response, error) {
			return &bootstrap.Response{SNI: map[string]string{"quic": "edge.example"}}, nil
		})
		selector, err := connection.NewProtocolSelector("quic", s.log.Logger())
		require.NoError(t, err)
		s.config.ProtocolSelector = selector

		var starts []time.Time
		s.edgeTunnelServer = regionChangeTunnelServer(func(_ context.Context, _ uint8, connected *signal.Signal) error {
			starts = append(starts, time.Now())
			connected.Notify()
			if len(starts) == 3 {
				cancel()
			}
			return connection.ErrRegionChanged
		})

		require.NoError(t, s.runWithRegionChanges(ctx, signal.New(make(chan struct{}))))
		require.Len(t, starts, 3)
		require.Equal(t, starts[0], starts[1], "the first region change must restart without a backoff delay")
		require.Equal(t, tunnelRetryDuration, starts[2].Sub(starts[1]))
	})
	require.InDelta(t, restartsBefore+2, regionChangeRestartCount(t), 0)
}

func regionChangeRestartCount(t *testing.T) float64 {
	t.Helper()
	var metric dto.Metric
	require.NoError(t, regionChangeRestarts.Write(&metric))
	return metric.GetCounter().GetValue()
}

func TestRegionChangeCancellationInterruptsRestartDelay(t *testing.T) {
	t.Parallel()

	s := newTestSupervisor(t, nil)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 2*tunnelRetryDuration)
		defer cancel()
		s.config.NamedTunnel = &connection.TunnelProperties{}
		s.config.HAConnections = 1
		s.bootstrapClient = bootstrapFetcherFunc(func(context.Context, connection.Credentials, *client.Config) (*bootstrap.Response, error) {
			return &bootstrap.Response{SNI: map[string]string{"quic": "edge.example"}}, nil
		})
		selector, err := connection.NewProtocolSelector("quic", s.log.Logger())
		require.NoError(t, err)
		s.config.ProtocolSelector = selector

		var starts int
		s.edgeTunnelServer = regionChangeTunnelServer(func(_ context.Context, _ uint8, connected *signal.Signal) error {
			starts++
			connected.Notify()
			return connection.ErrRegionChanged
		})
		done := make(chan error, 1)
		go func() { done <- s.runWithRegionChanges(ctx, signal.New(make(chan struct{}))) }()

		// Let the immediate restart complete and wait for the next restart delay.
		synctest.Wait()
		require.Equal(t, 2, starts)
		stoppedAt := time.Now()
		cancel()

		require.NoError(t, <-done)
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.Equal(t, stoppedAt, time.Now(), "cancellation must interrupt the restart delay")
		require.Equal(t, 2, starts, "cancellation must prevent another connection")
	})
}

func TestRegionChangeGracefulShutdownInterruptsRestartDelay(t *testing.T) {
	t.Parallel()

	s := newTestSupervisor(t, nil)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 2*tunnelRetryDuration)
		defer cancel()
		shutdown := make(chan struct{})
		s.gracefulShutdownC = shutdown
		s.config.NamedTunnel = &connection.TunnelProperties{}
		s.config.HAConnections = 1
		s.bootstrapClient = bootstrapFetcherFunc(func(context.Context, connection.Credentials, *client.Config) (*bootstrap.Response, error) {
			return &bootstrap.Response{SNI: map[string]string{"quic": "edge.example"}}, nil
		})
		selector, err := connection.NewProtocolSelector("quic", s.log.Logger())
		require.NoError(t, err)
		s.config.ProtocolSelector = selector

		var starts int
		s.edgeTunnelServer = regionChangeTunnelServer(func(_ context.Context, _ uint8, connected *signal.Signal) error {
			starts++
			connected.Notify()
			return connection.ErrRegionChanged
		})
		done := make(chan error, 1)
		go func() { done <- s.runWithRegionChanges(ctx, signal.New(make(chan struct{}))) }()

		// Let the immediate restart complete and wait for the next restart delay.
		synctest.Wait()
		require.Equal(t, 2, starts)
		stoppedAt := time.Now()
		close(shutdown)

		require.NoError(t, <-done)
		require.NoError(t, ctx.Err(), "graceful shutdown must leave the parent context active")
		require.Equal(t, stoppedAt, time.Now(), "graceful shutdown must interrupt the restart delay")
		require.Equal(t, 2, starts, "graceful shutdown must prevent another connection")
	})
}
