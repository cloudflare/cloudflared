package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	conn "github.com/cloudflare/cloudflared/connection"

	"github.com/rs/zerolog"
)

// ReadyServer serves HTTP 200 if the tunnel can serve traffic. Intended for k8s readiness checks.
type ReadyServer struct {
	sync.RWMutex
	isConnected map[int]bool
	log         *zerolog.Logger
}

// NewReadyServer initializes a ReadyServer and starts listening for dis/connection events.
func NewReadyServer(log *zerolog.Logger) *ReadyServer {
	return &ReadyServer{
		isConnected: make(map[int]bool, 0),
		log:         log,
	}
}

func (rs *ReadyServer) OnTunnelEvent(c conn.Event) {
	switch c.EventType {
	case conn.Connected:
		rs.Lock()
		rs.isConnected[int(c.Index)] = true
		rs.Unlock()
	case conn.Disconnected, conn.Reconnecting, conn.RegisteringTunnel, conn.Unregistering:
		rs.Lock()
		rs.isConnected[int(c.Index)] = false
		rs.Unlock()
	case conn.SetURL:
		break
	default:
		rs.log.Error().Msgf("Unknown connection event case %v", c)
	}
}

type body struct {
	Status           int `json:"status"`
	ReadyConnections int `json:"readyConnections"`
}

// ServeHTTP responds with HTTP 200 if the tunnel is connected to the edge.
func (rs *ReadyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	statusCode, readyConnections := rs.makeResponse()
	w.WriteHeader(statusCode)
	body := body{
		Status:           statusCode,
		ReadyConnections: readyConnections,
	}
	msg, err := json.Marshal(body)
	if err != nil {
		_, _ = fmt.Fprintf(w, `{"error": "%s"}`, err)
	}
	_, _ = w.Write(msg)
}

// This is the bulk of the logic for ServeHTTP, broken into its own pure function
// to make unit testing easy.
func (rs *ReadyServer) makeResponse() (statusCode, readyConnections int) {
	statusCode = http.StatusServiceUnavailable
	rs.RLock()
	defer rs.RUnlock()
	for _, connected := range rs.isConnected {
		if connected {
			statusCode = http.StatusOK
			readyConnections++
		}
	}
	return statusCode, readyConnections
}
