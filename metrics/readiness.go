package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	conn "github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

// ReadyServer serves HTTP 200 if the tunnel can serve traffic. Intended for k8s readiness checks.
type ReadyServer struct {
	clientID uuid.UUID
	tracker  *tunnelstate.ConnTracker
}

// NewReadyServer initializes a ReadyServer and starts listening for dis/connection events.
func NewReadyServer(log *zerolog.Logger, clientID uuid.UUID) *ReadyServer {
	return &ReadyServer{
		clientID: clientID,
		tracker:  tunnelstate.NewConnTracker(log),
	}
}

func (rs *ReadyServer) OnTunnelEvent(c conn.Event) {
	rs.tracker.OnTunnelEvent(c)
}

type body struct {
	Status           int       `json:"status"`
	ReadyConnections uint      `json:"readyConnections"`
	ConnectorID      uuid.UUID `json:"connectorId"`
}

// ServeHTTP responds with HTTP 200 if the tunnel is connected to the edge.
func (rs *ReadyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	statusCode, readyConnections := rs.makeResponse()
	w.WriteHeader(statusCode)
	body := body{
		Status:           statusCode,
		ReadyConnections: readyConnections,
		ConnectorID:      rs.clientID,
	}
	msg, err := json.Marshal(body)
	if err != nil {
		_, _ = fmt.Fprintf(w, `{"error": "%s"}`, err)
	}
	_, _ = w.Write(msg)
}

// This is the bulk of the logic for ServeHTTP, broken into its own pure function
// to make unit testing easy.
func (rs *ReadyServer) makeResponse() (statusCode int, readyConnections uint) {
	readyConnections = rs.tracker.CountActiveConns()
	if readyConnections > 0 {
		return http.StatusOK, readyConnections
	} else {
		return http.StatusServiceUnavailable, readyConnections
	}
}
