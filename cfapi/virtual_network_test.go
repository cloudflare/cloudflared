package cfapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestVirtualNetworkJsonRoundtrip(t *testing.T) {
	data := `{
		"id":"74fce949-351b-4752-b261-81a56cfd3130",
		"comment":"New York DC1",
		"name":"us-east-1",
		"is_default_network":true,
		"created_at":"2021-11-26T14:40:02.600673Z",
		"deleted_at":"2021-12-01T10:23:13.102645Z"
	}`
	var v VirtualNetwork
	err := json.Unmarshal([]byte(data), &v)

	require.NoError(t, err)
	require.Equal(t, uuid.MustParse("74fce949-351b-4752-b261-81a56cfd3130"), v.ID)
	require.Equal(t, "us-east-1", v.Name)
	require.Equal(t, "New York DC1", v.Comment)
	require.Equal(t, true, v.IsDefault)

	bytes, err := json.Marshal(v)
	require.NoError(t, err)
	obtainedJson := string(bytes)
	data = strings.Replace(data, "\t", "", -1)
	data = strings.Replace(data, "\n", "", -1)
	require.Equal(t, data, obtainedJson)
}

func TestMarshalNewVnet(t *testing.T) {
	newVnet := NewVirtualNetwork{
		Name:      "eu-west-1",
		Comment:   "London office",
		IsDefault: true,
	}

	serialized, err := json.Marshal(newVnet)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(serialized), newVnet.Name))
}

func TestMarshalUpdateVnet(t *testing.T) {
	newName := "bulgaria-1"
	updates := UpdateVirtualNetwork{
		Name: &newName,
	}

	// Test where receiver is struct
	serialized, err := json.Marshal(updates)
	require.NoError(t, err)
	require.True(t, strings.Contains(string(serialized), newName))
}

func TestVnetTableString(t *testing.T) {
	virtualNet := VirtualNetwork{
		ID:        uuid.New(),
		Name:      "us-east-1",
		Comment:   "New York DC1",
		IsDefault: true,
		CreatedAt: time.Now(),
		DeletedAt: time.Time{},
	}

	row := virtualNet.TableString()
	require.True(t, strings.HasPrefix(row, virtualNet.ID.String()))
	require.True(t, strings.Contains(row, virtualNet.Name))
	require.True(t, strings.Contains(row, virtualNet.Comment))
	require.True(t, strings.Contains(row, "true"))
	require.True(t, strings.HasSuffix(row, "-\t"))
}
