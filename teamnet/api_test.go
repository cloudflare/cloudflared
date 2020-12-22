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
	err := r.UnmarshalJSON([]byte(data))

	// Check everything worked
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse("fba6ffea-807f-4e7a-a740-4184ee1b82c8"), r.TunnelID)
	require.Equal(t, "test", r.Comment)
	_, cidr, err := net.ParseCIDR("10.1.2.40/29")
	require.NoError(t, err)
	require.Equal(t, *cidr, r.Network)
	require.Equal(t, "test", r.Comment)
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
	r := Route{
		Network: *network,
	}
	row := r.TableString()
	fmt.Println(row)
	require.True(t, strings.HasPrefix(row, "1.2.3.4/32"))
}
