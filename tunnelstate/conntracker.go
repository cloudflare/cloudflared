package tunnelstate

import (
	"sync"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
)

type ConnTracker struct {
	sync.RWMutex
	// int is the connection Index
	connectionInfo map[uint8]ConnectionInfo
	log            *zerolog.Logger
}

type ConnectionInfo struct {
	IsConnected bool
	Protocol    connection.Protocol
}

func NewConnTracker(log *zerolog.Logger) *ConnTracker {
	return &ConnTracker{
		connectionInfo: make(map[uint8]ConnectionInfo, 0),
		log:            log,
	}
}

func MockedConnTracker(mocked map[uint8]ConnectionInfo) *ConnTracker {
	return &ConnTracker{
		connectionInfo: mocked,
	}
}

func (ct *ConnTracker) OnTunnelEvent(c connection.Event) {
	switch c.EventType {
	case connection.Connected:
		ct.Lock()
		ci := ConnectionInfo{
			IsConnected: true,
			Protocol:    c.Protocol,
		}
		ct.connectionInfo[c.Index] = ci
		ct.Unlock()
	case connection.Disconnected, connection.Reconnecting, connection.RegisteringTunnel, connection.Unregistering:
		ct.Lock()
		ci := ct.connectionInfo[c.Index]
		ci.IsConnected = false
		ct.connectionInfo[c.Index] = ci
		ct.Unlock()
	default:
		ct.log.Error().Msgf("Unknown connection event case %v", c)
	}
}

func (ct *ConnTracker) CountActiveConns() uint {
	ct.RLock()
	defer ct.RUnlock()
	active := uint(0)
	for _, ci := range ct.connectionInfo {
		if ci.IsConnected {
			active++
		}
	}
	return active
}

// HasConnectedWith checks if we've ever had a successful connection to the edge
// with said protocol.
func (ct *ConnTracker) HasConnectedWith(protocol connection.Protocol) bool {
	ct.RLock()
	defer ct.RUnlock()
	for _, ci := range ct.connectionInfo {
		if ci.Protocol == protocol {
			return true
		}
	}
	return false
}
