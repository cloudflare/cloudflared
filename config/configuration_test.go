package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"
)

func TestConfigFileSettings(t *testing.T) {
	var (
		firstIngress = UnvalidatedIngressRule{
			Hostname: "tunnel1.example.com",
			Path:     "/id",
			Service:  "https://localhost:8000",
		}
		secondIngress = UnvalidatedIngressRule{
			Hostname: "*",
			Path:     "",
			Service:  "https://localhost:8001",
		}
		warpRouting = WarpRoutingConfig{
			Enabled:        true,
			ConnectTimeout: &CustomDuration{Duration: 2 * time.Second},
			TCPKeepAlive:   &CustomDuration{Duration: 10 * time.Second},
		}
	)
	rawYAML := `
tunnel: config-file-test
originRequest:
  ipRules:
  - prefix: "10.0.0.0/8"
    ports:
    - 80
    - 8080
    allow: false
  - prefix: "fc00::/7"
    ports:
    - 443
    - 4443
    allow: true
ingress:
 - hostname: tunnel1.example.com
   path: /id
   service: https://localhost:8000
 - hostname: "*"
   service: https://localhost:8001
warp-routing: 
  enabled: true
  connectTimeout: 2s
  tcpKeepAlive: 10s

retries: 5
grace-period: 30s
percentage: 3.14
hostname: example.com
tag:
 - test
 - central-1
counters:
 - 123
 - 456
`
	var config configFileSettings
	err := yaml.Unmarshal([]byte(rawYAML), &config)
	assert.NoError(t, err)

	assert.Equal(t, "config-file-test", config.TunnelID)
	assert.Equal(t, firstIngress, config.Ingress[0])
	assert.Equal(t, secondIngress, config.Ingress[1])
	assert.Equal(t, warpRouting, config.WarpRouting)
	privateV4 := "10.0.0.0/8"
	privateV6 := "fc00::/7"
	ipRules := []IngressIPRule{
		{
			Prefix: &privateV4,
			Ports:  []int{80, 8080},
			Allow:  false,
		},
		{
			Prefix: &privateV6,
			Ports:  []int{443, 4443},
			Allow:  true,
		},
	}
	assert.Equal(t, ipRules, config.OriginRequest.IPRules)

	retries, err := config.Int("retries")
	assert.NoError(t, err)
	assert.Equal(t, 5, retries)

	gracePeriod, err := config.Duration("grace-period")
	assert.NoError(t, err)
	assert.Equal(t, time.Second*30, gracePeriod)

	percentage, err := config.Float64("percentage")
	assert.NoError(t, err)
	assert.Equal(t, 3.14, percentage)

	hostname, err := config.String("hostname")
	assert.NoError(t, err)
	assert.Equal(t, "example.com", hostname)

	tags, err := config.StringSlice("tag")
	assert.NoError(t, err)
	assert.Equal(t, "test", tags[0])
	assert.Equal(t, "central-1", tags[1])

	counters, err := config.IntSlice("counters")
	assert.NoError(t, err)
	assert.Equal(t, 123, counters[0])
	assert.Equal(t, 456, counters[1])

}

var rawJsonConfig = []byte(`
{
	"connectTimeout": 10,
	"tlsTimeout": 30,
	"tcpKeepAlive": 30,
	"noHappyEyeballs": true,
	"keepAliveTimeout": 60,
	"keepAliveConnections": 10,
	"httpHostHeader": "app.tunnel.com",
	"originServerName": "app.tunnel.com",
	"caPool": "/etc/capool",
	"noTLSVerify": true,
	"disableChunkedEncoding": true,
	"bastionMode": true,
	"proxyAddress": "127.0.0.3",
	"proxyPort": 9000,
	"proxyType": "socks",
	"ipRules": [
		{
			"prefix": "10.0.0.0/8",
			"ports": [80, 8080],
			"allow": false
		},
		{
			"prefix": "fc00::/7",
			"ports": [443, 4443],
			"allow": true
		}
	],
	"http2Origin": true
}
`)

func TestMarshalUnmarshalOriginRequest(t *testing.T) {
	testCases := []struct {
		name          string
		marshalFunc   func(in interface{}) (out []byte, err error)
		unMarshalFunc func(in []byte, out interface{}) (err error)
	}{
		{"json", json.Marshal, json.Unmarshal},
		{"yaml", yaml.Marshal, yaml.Unmarshal},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assertConfig(t, tc.marshalFunc, tc.unMarshalFunc)
		})
	}
}

func assertConfig(
	t *testing.T,
	marshalFunc func(in interface{}) (out []byte, err error),
	unMarshalFunc func(in []byte, out interface{}) (err error),
) {
	var config OriginRequestConfig
	var config2 OriginRequestConfig

	assert.NoError(t, json.Unmarshal(rawJsonConfig, &config))

	assert.Equal(t, time.Second*10, config.ConnectTimeout.Duration)
	assert.Equal(t, time.Second*30, config.TLSTimeout.Duration)
	assert.Equal(t, time.Second*30, config.TCPKeepAlive.Duration)
	assert.Equal(t, true, *config.NoHappyEyeballs)
	assert.Equal(t, time.Second*60, config.KeepAliveTimeout.Duration)
	assert.Equal(t, 10, *config.KeepAliveConnections)
	assert.Equal(t, "app.tunnel.com", *config.HTTPHostHeader)
	assert.Equal(t, "app.tunnel.com", *config.OriginServerName)
	assert.Equal(t, "/etc/capool", *config.CAPool)
	assert.Equal(t, true, *config.NoTLSVerify)
	assert.Equal(t, true, *config.DisableChunkedEncoding)
	assert.Equal(t, true, *config.BastionMode)
	assert.Equal(t, "127.0.0.3", *config.ProxyAddress)
	assert.Equal(t, true, *config.NoTLSVerify)
	assert.Equal(t, uint(9000), *config.ProxyPort)
	assert.Equal(t, "socks", *config.ProxyType)
	assert.Equal(t, true, *config.Http2Origin)

	privateV4 := "10.0.0.0/8"
	privateV6 := "fc00::/7"
	ipRules := []IngressIPRule{
		{
			Prefix: &privateV4,
			Ports:  []int{80, 8080},
			Allow:  false,
		},
		{
			Prefix: &privateV6,
			Ports:  []int{443, 4443},
			Allow:  true,
		},
	}
	assert.Equal(t, ipRules, config.IPRules)

	// validate that serializing and deserializing again matches the deserialization from raw string
	result, err := marshalFunc(config)
	require.NoError(t, err)
	err = unMarshalFunc(result, &config2)
	require.NoError(t, err)

	require.Equal(t, config2, config)
}
