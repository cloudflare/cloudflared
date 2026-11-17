package supervisor

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/signal"
)

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

func TestRegionChangeDrainsHABeforeReconnecting(t *testing.T) {
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

			server, calls, _ := newRegionChangeTunnelServer(t, ctx, cancel, haConnections, notification.err)

			s := newTestSupervisor(t, server)
			s.tunnelsConnecting = make(map[int]chan struct{})
			s.config.HAConnections = haConnections

			selector, err := connection.NewProtocolSelector("quic", s.log.Logger())
			require.NoError(t, err)
			s.config.ProtocolSelector = selector

			require.NoError(t, s.runWithRegionChanges(ctx, signal.New(make(chan struct{}))))
			require.Equal(t, int32(2*haConnections), calls.Load(), "expected the original and replacement HA groups")
		})
	}
}

func TestRegionChangeRestartInterval(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"repeated changes", "cancellation", "graceful shutdown"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			s := newTestSupervisor(t, nil)
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 2*tunnelRetryDuration)
				defer cancel()
				shutdown := make(chan struct{})
				var starts []time.Time
				server := regionChangeTunnelServer(func(_ context.Context, _ uint8, connected *signal.Signal) error {
					starts = append(starts, time.Now())
					connected.Notify()
					if len(starts) == 3 {
						cancel()
					} else if len(starts) == 2 && mode != "repeated changes" {
						time.AfterFunc(time.Second, func() {
							if mode == "graceful shutdown" {
								close(shutdown)
							} else {
								cancel()
							}
						})
					}
					return connection.ErrRegionChanged
				})
				s.edgeTunnelServer = server
				s.config.HAConnections = 1
				s.gracefulShutdownC = shutdown
				selector, err := connection.NewProtocolSelector("quic", s.log.Logger())
				require.NoError(t, err)
				s.config.ProtocolSelector = selector

				require.NoError(t, s.runWithRegionChanges(ctx, signal.New(make(chan struct{}))))
				if mode == "repeated changes" {
					require.Len(t, starts, 3)
					require.GreaterOrEqual(t, starts[2].Sub(starts[1]), tunnelRetryDuration)
				} else {
					require.Len(t, starts, 2, "shutdown must interrupt the restart delay")
					if mode == "graceful shutdown" {
						require.NoError(t, ctx.Err())
					} else {
						require.ErrorIs(t, ctx.Err(), context.Canceled)
					}
				}
				require.Equal(t, starts[0], starts[1], "the first region change must restart immediately")
			})
		})
	}
}
