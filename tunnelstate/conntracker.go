package tunnelstate

import (
	"net"
	"sync"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/connection"
)

type ConnTracker struct {
	mutex sync.RWMutex
	// int is the connection Index
	connectionInfo map[uint8]ConnectionInfo
	log            *zerolog.Logger
}

type ConnectionInfo struct {
	IsConnected bool                `json:"isConnected,omitempty"`
	Protocol    connection.Protocol `json:"protocol,omitempty"`
	EdgeAddress net.IP              `json:"edgeAddress,omitempty"`
}

// Convinience struct to extend the connection with its index.
type IndexedConnectionInfo struct {
	ConnectionInfo
	Index uint8 `json:"index,omitempty"`
}

func NewConnTracker(
	log *zerolog.Logger,
) *ConnTracker {
	return &ConnTracker{
		connectionInfo: make(map[uint8]ConnectionInfo, 0),
		log:            log,
	}
}

func (ct *ConnTracker) OnTunnelEvent(c connection.Event) {
	switch c.EventType {
	case connection.Connected:
		ct.mutex.Lock()
		ci := ConnectionInfo{
			IsConnected: true,
			Protocol:    c.Protocol,
			EdgeAddress: c.EdgeAddress,
		}
		ct.connectionInfo[c.Index] = ci
		ct.mutex.Unlock()
	case connection.Disconnected, connection.Reconnecting, connection.RegisteringTunnel, connection.Unregistering:
		ct.mutex.Lock()
		ci := ct.connectionInfo[c.Index]
		ci.IsConnected = false
		ct.connectionInfo[c.Index] = ci
		ct.mutex.Unlock()
	default:
		ct.log.Error().Msgf("Unknown connection event case %v", c)
	}
}

func (ct *ConnTracker) CountActiveConns() uint {
	ct.mutex.RLock()
	defer ct.mutex.RUnlock()
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
	ct.mutex.RLock()
	defer ct.mutex.RUnlock()
	for _, ci := range ct.connectionInfo {
		if ci.Protocol == protocol {
			return true
		}
	}
	return false
}

// Returns the connection information iff it is connected this
// also leverages the [IndexedConnectionInfo] to also provide the connection index
func (ct *ConnTracker) GetActiveConnections() []IndexedConnectionInfo {
	ct.mutex.RLock()
	defer ct.mutex.RUnlock()

	connections := make([]IndexedConnectionInfo, 0)

	for key, value := range ct.connectionInfo {
		if value.IsConnected {
			info := IndexedConnectionInfo{value, key}
			connections = append(connections, info)
		}
	}

	return connections
}
