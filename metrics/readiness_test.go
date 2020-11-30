package metrics

import (
	"net/http"
	"testing"
)

func TestReadyServer_makeResponse(t *testing.T) {
	type fields struct {
		isConnected map[int]bool
	}
	tests := []struct {
		name                 string
		fields               fields
		wantOK               bool
		wantReadyConnections int
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
				isConnected: tt.fields.isConnected,
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
