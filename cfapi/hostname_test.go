package cfapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
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

func TestRouteTunnel_ZoneResolution(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zones" {
			// A sample JSON response matching the Cloudflare api format, ensuring we mimic what the real API sends.
			io.WriteString(w, `{
				"success": true,
				"errors": [],
				"messages": [],
				"result": [
					{
						"id": "zone-1",
						"name": "example.com",
						"status": "active",
						"paused": false
					},
					{
						"id": "zone-2",
						"name": "example.co.uk",
						"status": "active",
						"paused": false
					}
				],
				"result_info": {
					"page": 1,
					"per_page": 50,
					"total_pages": 1,
					"count": 2,
					"total_count": 2
				}
			}`)
			return
		}
		if r.URL.Path == "/zones/zone-1/tunnels/11111111-2222-3333-4444-555555555555/routes" {
			io.WriteString(w, `{"success":true,"result":{"cname":"new","name":"app.example.com"}}`)
			return
		}
		
		// Fallback path when zone does NOT match. It uses "default-zone-from-login" as specified in NewRESTClient arguments.
		if r.URL.Path == "/zones/default-zone-from-login/tunnels/11111111-2222-3333-4444-555555555555/routes" {
			io.WriteString(w, `{"success":true,"result":{"cname":"new","name":"fallback.otherdomain.com"}}`)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	logger := zerolog.Nop()
	client, err := NewRESTClient(ts.URL, "account", "default-zone-from-login", "token", "agent", &logger)
	assert.NoError(t, err)

	tunnelID, _ := uuid.Parse("11111111-2222-3333-4444-555555555555")

	t.Run("Success match", func(t *testing.T) {
		route := NewDNSRoute("app.example.com", false)
		res, err := client.RouteTunnel(tunnelID, route)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		assert.Equal(t, "Added CNAME app.example.com which will route to this tunnel", res.SuccessSummary())
	})

	t.Run("Fallback to default zone when no match", func(t *testing.T) {
		route := NewDNSRoute("fallback.otherdomain.com", false)
		res, err := client.RouteTunnel(tunnelID, route)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		assert.Equal(t, "Added CNAME fallback.otherdomain.com which will route to this tunnel", res.SuccessSummary())
	})
}
