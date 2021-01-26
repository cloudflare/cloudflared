package teamnet

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
	// Response from the teamnet route backend
	data := `{
		"network":"10.1.2.40/29",
		"tunnel_id":"fba6ffea-807f-4e7a-a740-4184ee1b82c8",
		"comment":"test",
		"created_at":"2020-12-22T02:00:15.587008Z",
		"deleted_at":null
	}`
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
}

func TestDetailedRouteJsonRoundtrip(t *testing.T) {
	// Response from the teamnet route backend
	data := `{
		"network":"10.1.2.40/29",
		"tunnel_id":"fba6ffea-807f-4e7a-a740-4184ee1b82c8",
		"comment":"test",
		"created_at":"2020-12-22T02:00:15.587008Z",
		"deleted_at":"2021-01-14T05:01:42.183002Z",
		"tunnel_name":"Mr. Tun"
	}`
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

	bytes, err := json.Marshal(r)
	require.NoError(t, err)
	obtainedJson := string(bytes)
	data = strings.Replace(data, "\t", "", -1)
	data = strings.Replace(data, "\n", "", -1)
	require.Equal(t, data, obtainedJson)
}

func TestMarshalNewRoute(t *testing.T) {
	_, network, err := net.ParseCIDR("1.2.3.4/32")
	require.NoError(t, err)
	require.NotNil(t, network)
	newRoute := NewRoute{
		Network:  *network,
		TunnelID: uuid.New(),
		Comment:  "hi",
	}

	// Test where receiver is struct
	serialized, err := json.Marshal(newRoute)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(serialized), "tunnel_id"))

	// Test where receiver is pointer to struct
	serialized, err = json.Marshal(&newRoute)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(serialized), "tunnel_id"))
}

func TestRouteTableString(t *testing.T) {
	_, network, err := net.ParseCIDR("1.2.3.4/32")
	require.NoError(t, err)
	require.NotNil(t, network)
	r := DetailedRoute{
		Network: CIDR(*network),
	}
	row := r.TableString()
	fmt.Println(row)
	require.True(t, strings.HasPrefix(row, "1.2.3.4/32"))
}
