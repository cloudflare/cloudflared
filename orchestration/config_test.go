package orchestration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ingress"
)

// TestNewLocalConfig_MarshalJSON tests that we are able to converte a compiled and validated config back
// into an "unvalidated" format which is compatible with Remote Managed configurations.
func TestNewLocalConfig_MarshalJSON(t *testing.T) {
	rawConfig := []byte(`
	{
		"originRequest": {
					"connectTimeout": 160,
					"httpHostHeader": "default"
		},
		"ingress": [
			{
				"hostname": "tun.example.com",
				"service": "https://localhost:8000"
			},
			{
				"hostname": "*",
				"service": "https://localhost:8001",
				"originRequest": {
					"connectTimeout": 121,
					"tlsTimeout": 2,
					"noHappyEyeballs": false,
					"tcpKeepAlive": 2,
					"keepAliveConnections": 2,
					"keepAliveTimeout": 2,
					"httpHostHeader": "def",
					"originServerName": "b2",
					"caPool": "/tmp/path1",
					"noTLSVerify": false,
					"disableChunkedEncoding": false,
					"bastionMode": false,
					"proxyAddress": "interface",
					"proxyPort": 200,
					"proxyType": "",
					"ipRules": [
						{
							"prefix": "10.0.0.0/16",
							"ports": [3000, 3030],
							"allow": false
						},
						{
							"prefix": "192.16.0.0/24",
							"ports": [5000, 5050],
							"allow": true
						}
					]
				}
			}
		],
        "warp-routing": {
			"enabled": true,
			"connectTimeout": 1
        }
	}
	`)

	var expectedConfig ingress.RemoteConfig
	err := json.Unmarshal(rawConfig, &expectedConfig)
	require.NoError(t, err)

	c := &newLocalConfig{
		RemoteConfig:       expectedConfig,
		ConfigurationFlags: nil,
	}

	jsonSerde, err := json.Marshal(c)
	require.NoError(t, err)

	var remoteConfig ingress.RemoteConfig
	err = json.Unmarshal(jsonSerde, &remoteConfig)
	require.NoError(t, err)

	require.Equal(t, remoteConfig.WarpRouting, ingress.WarpRoutingConfig{
		Enabled: true,
		ConnectTimeout: config.CustomDuration{
			Duration: time.Second,
		},
		TCPKeepAlive: config.CustomDuration{
			Duration: 30 * time.Second, // default value is 30 seconds
		},
	})
	require.Equal(t, remoteConfig.Ingress.Rules, expectedConfig.Ingress.Rules)
}
