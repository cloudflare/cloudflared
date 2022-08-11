package tunnelstate

import (
	"sync"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
)

type ConnTracker struct {
	sync.RWMutex
	isConnected map[int]bool
	log         *zerolog.Logger
}

func NewConnTracker(log *zerolog.Logger) *ConnTracker {
	return &ConnTracker{
		isConnected: make(map[int]bool, 0),
		log:         log,
	}
}

func MockedConnTracker(mocked map[int]bool) *ConnTracker {
	return &ConnTracker{
		isConnected: mocked,
	}
}

func (ct *ConnTracker) OnTunnelEvent(c connection.Event) {
	switch c.EventType {
	case connection.Connected:
		ct.Lock()
		ct.isConnected[int(c.Index)] = true
		ct.Unlock()
	case connection.Disconnected, connection.Reconnecting, connection.RegisteringTunnel, connection.Unregistering:
		ct.Lock()
		ct.isConnected[int(c.Index)] = false
		ct.Unlock()
	default:
		ct.log.Error().Msgf("Unknown connection event case %v", c)
	}
}

func (ct *ConnTracker) CountActiveConns() uint {
	ct.RLock()
	defer ct.RUnlock()
	active := uint(0)
	for _, connected := range ct.isConnected {
		if connected {
			active++
		}
	}
	return active
}
