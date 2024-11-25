package metrics_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/metrics"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

func mockRequest(t *testing.T, readyServer *metrics.ReadyServer) (int, uint) {
	t.Helper()

	var readyreadyConnections struct {
		Status           int       `json:"status"`
		ReadyConnections uint      `json:"readyConnections"`
		ConnectorID      uuid.UUID `json:"connectorId"`
	}
	rec := httptest.NewRecorder()
	readyServer.ServeHTTP(rec, nil)

	decoder := json.NewDecoder(rec.Body)
	err := decoder.Decode(&readyreadyConnections)
	require.NoError(t, err)
	return rec.Code, readyreadyConnections.ReadyConnections
}

func TestReadinessEventHandling(t *testing.T) {
	nopLogger := zerolog.Nop()
	tracker := tunnelstate.NewConnTracker(&nopLogger)
	rs := metrics.NewReadyServer(uuid.Nil, tracker)

	// start not ok
	code, readyConnections := mockRequest(t, rs)
	assert.NotEqualValues(t, http.StatusOK, code)
	assert.Zero(t, readyConnections)

	// one connected => ok
	tracker.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.Connected,
	})
	code, readyConnections = mockRequest(t, rs)
	assert.EqualValues(t, http.StatusOK, code)
	assert.EqualValues(t, 1, readyConnections)

	// another connected => still ok
	tracker.OnTunnelEvent(connection.Event{
		Index:     2,
		EventType: connection.Connected,
	})
	code, readyConnections = mockRequest(t, rs)
	assert.EqualValues(t, http.StatusOK, code)
	assert.EqualValues(t, 2, readyConnections)

	// one reconnecting => still ok
	tracker.OnTunnelEvent(connection.Event{
		Index:     2,
		EventType: connection.Reconnecting,
	})
	code, readyConnections = mockRequest(t, rs)
	assert.EqualValues(t, http.StatusOK, code)
	assert.EqualValues(t, 1, readyConnections)

	// Regression test for TUN-3777
	tracker.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.RegisteringTunnel,
	})
	code, readyConnections = mockRequest(t, rs)
	assert.NotEqualValues(t, http.StatusOK, code)
	assert.Zero(t, readyConnections)

	// other connected then unregistered  => not ok
	tracker.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.Connected,
	})
	code, readyConnections = mockRequest(t, rs)
	assert.EqualValues(t, http.StatusOK, code)
	assert.EqualValues(t, 1, readyConnections)
	tracker.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.Unregistering,
	})
	code, readyConnections = mockRequest(t, rs)
	assert.NotEqualValues(t, http.StatusOK, code)
	assert.Zero(t, readyConnections)

	// other disconnected => not ok
	tracker.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.Disconnected,
	})
	code, readyConnections = mockRequest(t, rs)
	assert.NotEqualValues(t, http.StatusOK, code)
	assert.Zero(t, readyConnections)
}
