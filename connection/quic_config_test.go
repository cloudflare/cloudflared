package connection

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewQUICConfig(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name              string
		addr              string
		initialPacketSize uint16
	}{
		{name: "IPv4", addr: "192.0.2.1:7844", initialPacketSize: 1232},
		{name: "IPv6", addr: "[2001:db8::1]:7844", initialPacketSize: 1252},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			addr := netip.MustParseAddrPort(tc.addr)
			options := QUICConfigOptions{
				DisablePathMTUDiscovery:    true,
				MaxConnectionReceiveWindow: 16 * 1024 * 1024,
				MaxStreamReceiveWindow:     8 * 1024 * 1024,
			}
			cfg := NewQUICConfig(addr, options)
			assert.Equal(t, tc.initialPacketSize, cfg.InitialPacketSize)
			assert.Equal(t, 5*time.Second, cfg.HandshakeIdleTimeout)
			assert.Equal(t, 5*time.Second, cfg.MaxIdleTimeout)
			assert.Equal(t, time.Second, cfg.KeepAlivePeriod)
			assert.EqualValues(t, 1<<60, cfg.MaxIncomingStreams)
			assert.EqualValues(t, 1<<60, cfg.MaxIncomingUniStreams)
			assert.True(t, cfg.EnableDatagrams)
			assert.True(t, cfg.DisablePathMTUDiscovery)
			assert.Equal(t, options.MaxConnectionReceiveWindow, cfg.MaxConnectionReceiveWindow)
			assert.Equal(t, options.MaxStreamReceiveWindow, cfg.MaxStreamReceiveWindow)

			// Per-connection changes must not leak into later dials or defaults.
			cfg.InitialPacketSize = 1200
			defaults := NewQUICConfig(addr, QUICConfigOptions{})
			assert.Equal(t, tc.initialPacketSize, defaults.InitialPacketSize)
			assert.False(t, defaults.DisablePathMTUDiscovery)
			assert.Zero(t, defaults.MaxConnectionReceiveWindow)
			assert.Zero(t, defaults.MaxStreamReceiveWindow)
		})
	}
}
