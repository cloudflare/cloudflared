package cfapi

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

var loc, _ = time.LoadLocation("UTC")

func Test_unmarshalTunnel(t *testing.T) {
	type args struct {
		body string
	}
	tests := []struct {
		name    string
		args    args
		want    *Tunnel
		wantErr bool
	}{
		{
			name: "empty list",
			args: args{body: `{"success": true, "result": {"id":"b34cc7ce-925b-46ee-bc23-4cb5c18d8292","created_at":"2021-07-29T13:46:14.090955Z","deleted_at":"2021-07-29T14:07:27.559047Z","name":"qt-bIWWN7D662ogh61pCPfu5s2XgqFY1OyV","account_id":6946212,"account_tag":"5ab4e9dfbd435d24068829fda0077963","conns_active_at":null,"conns_inactive_at":"2021-07-29T13:47:22.548482Z","tun_type":"cfd_tunnel","metadata":{"qtid":"a6fJROgkXutNruBGaJjD"}}}`},
			want: &Tunnel{
				ID:          uuid.MustParse("b34cc7ce-925b-46ee-bc23-4cb5c18d8292"),
				Name:        "qt-bIWWN7D662ogh61pCPfu5s2XgqFY1OyV",
				CreatedAt:   time.Date(2021, 07, 29, 13, 46, 14, 90955000, loc),
				DeletedAt:   time.Date(2021, 07, 29, 14, 7, 27, 559047000, loc),
				Connections: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unmarshalTunnel(strings.NewReader(tt.args.body))
			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshalTunnel() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unmarshalTunnel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnmarshalTunnelOk(t *testing.T) {

	jsonBody := `{"success": true, "result": {"id": "00000000-0000-0000-0000-000000000000","name":"test","created_at":"0001-01-01T00:00:00Z","connections":[]}}`
	expected := Tunnel{
		ID:          uuid.Nil,
		Name:        "test",
		CreatedAt:   time.Time{},
		Connections: []Connection{},
	}
	actual, err := unmarshalTunnel(bytes.NewReader([]byte(jsonBody)))
	assert.NoError(t, err)
	assert.Equal(t, &expected, actual)
}

func TestUnmarshalTunnelErr(t *testing.T) {

	tests := []string{
		`abc`,
		`{"success": true, "result": abc}`,
		`{"success": false, "result": {"id": "00000000-0000-0000-0000-000000000000","name":"test","created_at":"0001-01-01T00:00:00Z","connections":[]}}}`,
		`{"errors": [{"code": 1003, "message":"An A, AAAA or CNAME record already exists with that host"}], "result": {"id": "00000000-0000-0000-0000-000000000000","name":"test","created_at":"0001-01-01T00:00:00Z","connections":[]}}}`,
	}

	for i, test := range tests {
		_, err := unmarshalTunnel(bytes.NewReader([]byte(test)))
		assert.Error(t, err, fmt.Sprintf("Test #%v failed", i))
	}
}

func TestUnmarshalConnections(t *testing.T) {
	jsonBody := `{"success":true,"messages":[],"errors":[],"result":[{"id":"d4041254-91e3-4deb-bd94-b46e11680b1e","features":["ha-origin"],"version":"2021.2.5","arch":"darwin_amd64","conns":[{"colo_name":"LIS","id":"ac2286e5-c708-4588-a6a0-ba6b51940019","is_pending_reconnect":false,"origin_ip":"148.38.28.2","opened_at":"0001-01-01T00:00:00Z"}],"run_at":"0001-01-01T00:00:00Z"}]}`
	expected := ActiveClient{
		ID:       uuid.MustParse("d4041254-91e3-4deb-bd94-b46e11680b1e"),
		Features: []string{"ha-origin"},
		Version:  "2021.2.5",
		Arch:     "darwin_amd64",
		RunAt:    time.Time{},
		Connections: []Connection{{
			ID:                 uuid.MustParse("ac2286e5-c708-4588-a6a0-ba6b51940019"),
			ColoName:           "LIS",
			IsPendingReconnect: false,
			OriginIP:           net.ParseIP("148.38.28.2"),
			OpenedAt:           time.Time{},
		}},
	}
	actual, err := parseConnectionsDetails(bytes.NewReader([]byte(jsonBody)))
	assert.NoError(t, err)
	assert.Equal(t, []*ActiveClient{&expected}, actual)
}
