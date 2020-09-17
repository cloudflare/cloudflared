package tunnelstore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDNSRouteUnmarshalResult(t *testing.T) {
	route := &DNSRoute{
		userHostname: "example.com",
	}

	result, err := route.UnmarshalResult(strings.NewReader(`{"cname": "new"}`))

	assert.NoError(t, err)
	assert.Equal(t, &DNSRouteResult{
		route: route,
		CName: ChangeNew,
	}, result)

	_, err = route.UnmarshalResult(strings.NewReader(`abc`))
	assert.NotNil(t, err)
}

func TestLBRouteUnmarshalResult(t *testing.T) {
	route := &LBRoute{
		lbName: "lb.example.com",
		lbPool: "pool",
	}

	result, err := route.UnmarshalResult(strings.NewReader(`{"pool": "unchanged", "load_balancer": "updated"}`))

	assert.NoError(t, err)
	assert.Equal(t, &LBRouteResult{
		route:        route,
		LoadBalancer: ChangeUpdated,
		Pool:         ChangeUnchanged,
	}, result)

	_, err = route.UnmarshalResult(strings.NewReader(`abc`))
	assert.NotNil(t, err)
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
		{ChangeNew, ChangeNew, "Created load balancer lb.example.com and added a new pool POOL with this tunnel as an origin" },
		{ChangeNew, ChangeUpdated, "Created load balancer lb.example.com with an existing pool POOL which was updated to use this tunnel as an origin" },
		{ChangeNew, ChangeUnchanged, "Created load balancer lb.example.com with an existing pool POOL which already has this tunnel as an origin" },
		{ChangeUpdated, ChangeNew, "Added new pool POOL with this tunnel as an origin to load balancer lb.example.com" },
		{ChangeUpdated, ChangeUpdated, "Updated pool POOL to use this tunnel as an origin and added it to load balancer lb.example.com" },
		{ChangeUpdated, ChangeUnchanged, "Added pool POOL, which already has this tunnel as an origin, to load balancer lb.example.com" },
		{ChangeUnchanged, ChangeNew, "Something went wrong: failed to modify load balancer lb.example.com with pool POOL; please check traffic manager configuration in the dashboard" },
		{ChangeUnchanged, ChangeUpdated, "Added this tunnel as an origin in pool POOL which is already used by load balancer lb.example.com" },
		{ChangeUnchanged, ChangeUnchanged, "Load balancer lb.example.com already uses pool POOL which has this tunnel as an origin" },
		{"", "", "Something went wrong: failed to modify load balancer lb.example.com with pool POOL; please check traffic manager configuration in the dashboard" },
		{"a", "b", "Something went wrong: failed to modify load balancer lb.example.com with pool POOL; please check traffic manager configuration in the dashboard" },
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
