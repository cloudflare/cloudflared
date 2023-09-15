package cfapi

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalRoute(t *testing.T) {
	testCases := []struct {
		Json    string
		HasVnet bool
	}{
		{
			`{
				"network":"10.1.2.40/29",
				"tunnel_id":"fba6ffea-807f-4e7a-a740-4184ee1b82c8",
				"comment":"test",
				"created_at":"2020-12-22T02:00:15.587008Z",
				"deleted_at":null
			}`,
			false,
		},
		{
			`{
				"network":"10.1.2.40/29",
				"tunnel_id":"fba6ffea-807f-4e7a-a740-4184ee1b82c8",
				"comment":"test",
				"created_at":"2020-12-22T02:00:15.587008Z",
				"deleted_at":null,
				"virtual_network_id":"38c95083-8191-4110-8339-3f438d44fdb9"
			}`,
			true,
		},
	}

	for _, testCase := range testCases {
		data := testCase.Json

		var r Route
		err := json.Unmarshal([]byte(data), &r)

		// Check everything worked
		require.NoError(t, err)
		require.Equal(t, uuid.MustParse("fba6ffea-807f-4e7a-a740-4184ee1b82c8"), r.TunnelID)
		require.Equal(t, "test", r.Comment)
		_, cidr, err := net.ParseCIDR("10.1.2.40/29")
		require.NoError(t, err)
		require.Equal(t, CIDR(*cidr), r.Network)
		require.Equal(t, "test", r.Comment)

		if testCase.HasVnet {
			require.Equal(t, uuid.MustParse("38c95083-8191-4110-8339-3f438d44fdb9"), *r.VNetID)
		} else {
			require.Nil(t, r.VNetID)
		}
	}
}

func TestDetailedRouteJsonRoundtrip(t *testing.T) {
	testCases := []struct {
		Json    string
		HasVnet bool
	}{
		{
			`{
				"id":"91ebc578-cc99-4641-9937-0fb630505fa0",
				"network":"10.1.2.40/29",
				"tunnel_id":"fba6ffea-807f-4e7a-a740-4184ee1b82c8",
				"comment":"test",
				"created_at":"2020-12-22T02:00:15.587008Z",
				"deleted_at":"2021-01-14T05:01:42.183002Z",
				"tunnel_name":"Mr. Tun"
			}`,
			false,
		},
		{
			`{
				"id":"91ebc578-cc99-4641-9937-0fb630505fa0",
				"network":"10.1.2.40/29",
				"tunnel_id":"fba6ffea-807f-4e7a-a740-4184ee1b82c8",
				"virtual_network_id":"38c95083-8191-4110-8339-3f438d44fdb9",
				"comment":"test",
				"created_at":"2020-12-22T02:00:15.587008Z",
				"deleted_at":"2021-01-14T05:01:42.183002Z",
				"tunnel_name":"Mr. Tun"
			}`,
			true,
		},
	}

	for _, testCase := range testCases {
		data := testCase.Json

		var r DetailedRoute
		err := json.Unmarshal([]byte(data), &r)

		// Check everything worked
		require.NoError(t, err)
		require.Equal(t, uuid.MustParse("fba6ffea-807f-4e7a-a740-4184ee1b82c8"), r.TunnelID)
		require.Equal(t, "test", r.Comment)
		_, cidr, err := net.ParseCIDR("10.1.2.40/29")
		require.NoError(t, err)
		require.Equal(t, CIDR(*cidr), r.Network)
		require.Equal(t, "test", r.Comment)
		require.Equal(t, "Mr. Tun", r.TunnelName)

		if testCase.HasVnet {
			require.Equal(t, uuid.MustParse("38c95083-8191-4110-8339-3f438d44fdb9"), *r.VNetID)
		} else {
			require.Nil(t, r.VNetID)
		}

		bytes, err := json.Marshal(r)
		require.NoError(t, err)
		obtainedJson := string(bytes)
		data = strings.Replace(data, "\t", "", -1)
		data = strings.Replace(data, "\n", "", -1)
		require.Equal(t, data, obtainedJson)
	}
}

func TestMarshalNewRoute(t *testing.T) {
	_, network, err := net.ParseCIDR("1.2.3.4/32")
	require.NoError(t, err)
	require.NotNil(t, network)
	vnetId := uuid.New()

	newRoutes := []NewRoute{
		{
			Network:  *network,
			TunnelID: uuid.New(),
			Comment:  "hi",
		},
		{
			Network:  *network,
			TunnelID: uuid.New(),
			Comment:  "hi",
			VNetID:   &vnetId,
		},
	}

	for _, newRoute := range newRoutes {
		// Test where receiver is struct
		serialized, err := json.Marshal(newRoute)
		require.NoError(t, err)
		require.True(t, strings.Contains(string(serialized), "tunnel_id"))

		// Test where receiver is pointer to struct
		serialized, err = json.Marshal(&newRoute)
		require.NoError(t, err)
		require.True(t, strings.Contains(string(serialized), "tunnel_id"))

		if newRoute.VNetID == nil {
			require.False(t, strings.Contains(string(serialized), "virtual_network_id"))
		} else {
			require.True(t, strings.Contains(string(serialized), "virtual_network_id"))
		}
	}
}

func TestRouteTableString(t *testing.T) {
	_, network, err := net.ParseCIDR("1.2.3.4/32")
	require.NoError(t, err)
	require.NotNil(t, network)
	r := DetailedRoute{
		ID:      uuid.Nil,
		Network: CIDR(*network),
	}
	row := r.TableString()
	fmt.Println(row)
	require.True(t, strings.HasPrefix(row, "00000000-0000-0000-0000-000000000000\t1.2.3.4/32"))
}
