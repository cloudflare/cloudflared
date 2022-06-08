package metrics

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/tunnelstate"
)

func TestReadyServer_makeResponse(t *testing.T) {
	type fields struct {
		isConnected map[int]bool
	}
	tests := []struct {
		name                 string
		fields               fields
		wantOK               bool
		wantReadyConnections uint
	}{
		{
			name: "One connection online => HTTP 200",
			fields: fields{
				isConnected: map[int]bool{
					0: false,
					1: false,
					2: true,
					3: false,
				},
			},
			wantOK:               true,
			wantReadyConnections: 1,
		},
		{
			name: "No connections online => no HTTP 200",
			fields: fields{
				isConnected: map[int]bool{
					0: false,
					1: false,
					2: false,
					3: false,
				},
			},
			wantReadyConnections: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := &ReadyServer{
				tracker: tunnelstate.MockedConnTracker(tt.fields.isConnected),
			}
			gotStatusCode, gotReadyConnections := rs.makeResponse()
			if tt.wantOK && gotStatusCode != http.StatusOK {
				t.Errorf("ReadyServer.makeResponse() gotStatusCode = %v, want ok = %v", gotStatusCode, tt.wantOK)
			}
			if gotReadyConnections != tt.wantReadyConnections {
				t.Errorf("ReadyServer.makeResponse() gotReadyConnections = %v, want %v", gotReadyConnections, tt.wantReadyConnections)
			}
		})
	}
}

func TestReadinessEventHandling(t *testing.T) {
	nopLogger := zerolog.Nop()
	rs := NewReadyServer(&nopLogger, uuid.Nil)

	// start not ok
	code, ready := rs.makeResponse()
	assert.NotEqualValues(t, http.StatusOK, code)
	assert.Zero(t, ready)

	// one connected => ok
	rs.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.Connected,
	})
	code, ready = rs.makeResponse()
	assert.EqualValues(t, http.StatusOK, code)
	assert.EqualValues(t, 1, ready)

	// another connected => still ok
	rs.OnTunnelEvent(connection.Event{
		Index:     2,
		EventType: connection.Connected,
	})
	code, ready = rs.makeResponse()
	assert.EqualValues(t, http.StatusOK, code)
	assert.EqualValues(t, 2, ready)

	// one reconnecting => still ok
	rs.OnTunnelEvent(connection.Event{
		Index:     2,
		EventType: connection.Reconnecting,
	})
	code, ready = rs.makeResponse()
	assert.EqualValues(t, http.StatusOK, code)
	assert.EqualValues(t, 1, ready)

	// Regression test for TUN-3777
	rs.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.RegisteringTunnel,
	})
	code, ready = rs.makeResponse()
	assert.NotEqualValues(t, http.StatusOK, code)
	assert.Zero(t, ready)

	// other connected then unregistered  => not ok
	rs.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.Connected,
	})
	code, ready = rs.makeResponse()
	assert.EqualValues(t, http.StatusOK, code)
	assert.EqualValues(t, 1, ready)
	rs.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.Unregistering,
	})
	code, ready = rs.makeResponse()
	assert.NotEqualValues(t, http.StatusOK, code)
	assert.Zero(t, ready)

	// other disconnected => not ok
	rs.OnTunnelEvent(connection.Event{
		Index:     1,
		EventType: connection.Disconnected,
	})
	code, ready = rs.makeResponse()
	assert.NotEqualValues(t, http.StatusOK, code)
	assert.Zero(t, ready)
}
