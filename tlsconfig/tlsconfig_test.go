package tlsconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTunnelConfig(t *testing.T) {
	t.Parallel()

	tlsConfig, err := CreateTunnelConfig("testcert.pem", "edge.example.com")
	require.NoError(t, err)
	assert.Equal(t, "edge.example.com", tlsConfig.ServerName)
	assert.NotNil(t, tlsConfig.RootCAs)
	assert.Empty(t, tlsConfig.CurvePreferences)
}

func TestCreateTunnelConfigRequiresServerName(t *testing.T) {
	t.Parallel()

	_, err := CreateTunnelConfig("testcert.pem", "")
	assert.EqualError(t, err, "either ServerName or InsecureSkipVerify must be specified in the tls.Config")
}
