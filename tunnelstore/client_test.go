package tunnelstore

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDNSRouteUnmarshalResult(t *testing.T) {
	route := &DNSRoute{
		userHostname: "example.com",
	}

	result, err := route.UnmarshalResult(strings.NewReader(`{"success": true, "result": {"cname": "new"}}`))

	assert.NoError(t, err)
	assert.Equal(t, &DNSRouteResult{
		route: route,
		CName: ChangeNew,
	}, result)

	badJSON := []string{
		`abc`,
		`{"success": false, "result": {"cname": "new"}}`,
		`{"errors": [{"code": 1003, "message":"An A, AAAA or CNAME record already exists with that host"}], "result": {"cname": "new"}}`,
		`{"errors": [{"code": 1003, "message":"An A, AAAA or CNAME record already exists with that host"}, {"code": 1004, "message":"Cannot use tunnel as origin for non-proxied load balancer"}], "result": {"cname": "new"}}`,
		`{"result": {"cname": "new"}}`,
		`{"result": {"cname": "new"}}`,
	}

	for _, j := range badJSON {
		_, err = route.UnmarshalResult(strings.NewReader(j))
		assert.NotNil(t, err)
	}
}

func TestLBRouteUnmarshalResult(t *testing.T) {
	route := &LBRoute{
		lbName: "lb.example.com",
		lbPool: "pool",
	}

	result, err := route.UnmarshalResult(strings.NewReader(`{"success": true, "result": {"pool": "unchanged", "load_balancer": "updated"}}`))

	assert.NoError(t, err)
	assert.Equal(t, &LBRouteResult{
		route:        route,
		LoadBalancer: ChangeUpdated,
		Pool:         ChangeUnchanged,
	}, result)

	badJSON := []string{
		`abc`,
		`{"success": false, "result": {"pool": "unchanged", "load_balancer": "updated"}}`,
		`{"errors": [{"code": 1003, "message":"An A, AAAA or CNAME record already exists with that host"}], "result": {"pool": "unchanged", "load_balancer": "updated"}}`,
		`{"errors": [{"code": 1003, "message":"An A, AAAA or CNAME record already exists with that host"}, {"code": 1004, "message":"Cannot use tunnel as origin for non-proxied load balancer"}], "result": {"pool": "unchanged", "load_balancer": "updated"}}`,
		`{"result": {"pool": "unchanged", "load_balancer": "updated"}}`,
	}

	for _, j := range badJSON {
		_, err = route.UnmarshalResult(strings.NewReader(j))
		assert.NotNil(t, err)
	}
}

func TestLBRouteResultSuccessSummary(t *testing.T) {
	route := &LBRoute{
		lbName: "lb.example.com",
		lbPool: "POOL",
	}

	tests := []struct {
		lb       Change
		pool     Change
		expected string
	}{
		{ChangeNew, ChangeNew, "Created load balancer lb.example.com and added a new pool POOL with this tunnel as an origin"},
		{ChangeNew, ChangeUpdated, "Created load balancer lb.example.com with an existing pool POOL which was updated to use this tunnel as an origin"},
		{ChangeNew, ChangeUnchanged, "Created load balancer lb.example.com with an existing pool POOL which already has this tunnel as an origin"},
		{ChangeUpdated, ChangeNew, "Added new pool POOL with this tunnel as an origin to load balancer lb.example.com"},
		{ChangeUpdated, ChangeUpdated, "Updated pool POOL to use this tunnel as an origin and added it to load balancer lb.example.com"},
		{ChangeUpdated, ChangeUnchanged, "Added pool POOL, which already has this tunnel as an origin, to load balancer lb.example.com"},
		{ChangeUnchanged, ChangeNew, "Something went wrong: failed to modify load balancer lb.example.com with pool POOL; please check traffic manager configuration in the dashboard"},
		{ChangeUnchanged, ChangeUpdated, "Added this tunnel as an origin in pool POOL which is already used by load balancer lb.example.com"},
		{ChangeUnchanged, ChangeUnchanged, "Load balancer lb.example.com already uses pool POOL which has this tunnel as an origin"},
		{"", "", "Something went wrong: failed to modify load balancer lb.example.com with pool POOL; please check traffic manager configuration in the dashboard"},
		{"a", "b", "Something went wrong: failed to modify load balancer lb.example.com with pool POOL; please check traffic manager configuration in the dashboard"},
	}
	for i, tt := range tests {
		res := &LBRouteResult{
			route:        route,
			LoadBalancer: tt.lb,
			Pool:         tt.pool,
		}
		actual := res.SuccessSummary()
		assert.Equal(t, tt.expected, actual, "case %d", i+1)
	}
}

func Test_parseListTunnels(t *testing.T) {
	type args struct {
		body string
	}
	tests := []struct {
		name    string
		args    args
		want    []*Tunnel
		wantErr bool
	}{
		{
			name: "empty list",
			args: args{body: `{"success": true, "result": []}`},
			want: []*Tunnel{},
		},
		{
			name:    "success is false",
			args:    args{body: `{"success": false, "result": []}`},
			wantErr: true,
		},
		{
			name:    "errors are present",
			args:    args{body: `{"errors": [{"code": 1003, "message":"An A, AAAA or CNAME record already exists with that host"}], "result": []}`},
			wantErr: true,
		},
		{
			name:    "invalid response",
			args:    args{body: `abc`},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := ioutil.NopCloser(bytes.NewReader([]byte(tt.args.body)))
			got, err := parseListTunnels(body)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseListTunnels() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseListTunnels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_unmarshalTunnel(t *testing.T) {
	type args struct {
		reader io.Reader
	}
	tests := []struct {
		name    string
		args    args
		want    *Tunnel
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unmarshalTunnel(tt.args.reader)
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
