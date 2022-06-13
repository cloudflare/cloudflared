package ingress

import (
	"encoding/json"
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	yaml "gopkg.in/yaml.v3"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ipaccess"
)

// Ensure that the nullable config from `config` package and the
// non-nullable config from `ingress` package have the same number of
// fields.
// This test ensures that programmers didn't add a new field to
// one struct and forget to add it to the other ;)
func TestCorrespondingFields(t *testing.T) {
	require.Equal(
		t,
		CountFields(t, config.OriginRequestConfig{}),
		CountFields(t, OriginRequestConfig{}),
	)
}

func CountFields(t *testing.T, val interface{}) int {
	b, err := yaml.Marshal(val)
	require.NoError(t, err)
	m := make(map[string]interface{}, 0)
	err = yaml.Unmarshal(b, &m)
	require.NoError(t, err)
	return len(m)
}

func TestUnmarshalRemoteConfigOverridesGlobal(t *testing.T) {
	rawConfig := []byte(`
{
    "originRequest": {
        "connectTimeout": 90,
		"noHappyEyeballs": true
    },
    "ingress": [
        {
            "hostname": "jira.cfops.com",
            "service": "http://192.16.19.1:80",
            "originRequest": {
                "noTLSVerify": true,
                "connectTimeout": 10
            }
        },
        {
            "service": "http_status:404"
        }
    ],
    "warp-routing": {
        "enabled": true
    }
}	
`)
	var remoteConfig RemoteConfig
	err := json.Unmarshal(rawConfig, &remoteConfig)
	require.NoError(t, err)
	require.True(t, remoteConfig.Ingress.Rules[0].Config.NoTLSVerify)
	require.True(t, remoteConfig.Ingress.Defaults.NoHappyEyeballs)
}

func TestOriginRequestConfigOverrides(t *testing.T) {
	validate := func(ing Ingress) {
		// Rule 0 didn't override anything, so it inherits the user-specified
		// root-level configuration.
		actual0 := ing.Rules[0].Config
		expected0 := OriginRequestConfig{
			ConnectTimeout:         config.CustomDuration{Duration: 1 * time.Minute},
			TLSTimeout:             config.CustomDuration{Duration: 1 * time.Second},
			TCPKeepAlive:           config.CustomDuration{Duration: 1 * time.Second},
			NoHappyEyeballs:        true,
			KeepAliveTimeout:       config.CustomDuration{Duration: 1 * time.Second},
			KeepAliveConnections:   1,
			HTTPHostHeader:         "abc",
			OriginServerName:       "a1",
			CAPool:                 "/tmp/path0",
			NoTLSVerify:            true,
			DisableChunkedEncoding: true,
			BastionMode:            true,
			ProxyAddress:           "127.1.2.3",
			ProxyPort:              uint(100),
			ProxyType:              "socks5",
			IPRules: []ipaccess.Rule{
				newIPRule(t, "10.0.0.0/8", []int{80, 8080}, false),
				newIPRule(t, "fc00::/7", []int{443, 4443}, true),
			},
		}
		require.Equal(t, expected0, actual0)

		// Rule 1 overrode all the root-level config.
		actual1 := ing.Rules[1].Config
		expected1 := OriginRequestConfig{
			ConnectTimeout:         config.CustomDuration{Duration: 2 * time.Minute},
			TLSTimeout:             config.CustomDuration{Duration: 2 * time.Second},
			TCPKeepAlive:           config.CustomDuration{Duration: 2 * time.Second},
			NoHappyEyeballs:        false,
			KeepAliveTimeout:       config.CustomDuration{Duration: 2 * time.Second},
			KeepAliveConnections:   2,
			HTTPHostHeader:         "def",
			OriginServerName:       "b2",
			CAPool:                 "/tmp/path1",
			NoTLSVerify:            false,
			DisableChunkedEncoding: false,
			BastionMode:            false,
			ProxyAddress:           "interface",
			ProxyPort:              uint(200),
			ProxyType:              "",
			IPRules: []ipaccess.Rule{
				newIPRule(t, "10.0.0.0/16", []int{3000, 3030}, false),
				newIPRule(t, "192.16.0.0/24", []int{5000, 5050}, true),
			},
		}
		require.Equal(t, expected1, actual1)
	}

	rulesYAML := `
originRequest:
  connectTimeout: 1m
  tlsTimeout: 1s
  noHappyEyeballs: true
  tcpKeepAlive: 1s
  keepAliveConnections: 1
  keepAliveTimeout: 1s
  httpHostHeader: abc
  originServerName: a1
  caPool: /tmp/path0
  noTLSVerify: true
  disableChunkedEncoding: true
  bastionMode: True
  proxyAddress: 127.1.2.3
  proxyPort: 100
  proxyType: socks5
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
- hostname: tun.example.com
  service: https://localhost:8000
- hostname: "*"
  service: https://localhost:8001
  originRequest:
    connectTimeout: 2m
    tlsTimeout: 2s
    noHappyEyeballs: false
    tcpKeepAlive: 2s
    keepAliveConnections: 2
    keepAliveTimeout: 2s
    httpHostHeader: def
    originServerName: b2
    caPool: /tmp/path1
    noTLSVerify: false
    disableChunkedEncoding: false
    bastionMode: false
    proxyAddress: interface
    proxyPort: 200
    proxyType: ""
    ipRules:
    - prefix: "10.0.0.0/16"
      ports:
      - 3000
      - 3030
      allow: false
    - prefix: "192.16.0.0/24"
      ports:
      - 5000
      - 5050
      allow: true
`

	ing, err := ParseIngress(MustReadIngress(rulesYAML))
	require.NoError(t, err)
	validate(ing)

	rawConfig := []byte(`
{
    "originRequest": {
        "connectTimeout": 60,
		"tlsTimeout": 1,
		"noHappyEyeballs": true,
		"tcpKeepAlive": 1,
		"keepAliveConnections": 1,
		"keepAliveTimeout": 1,
		"httpHostHeader": "abc",
		"originServerName": "a1",
		"caPool": "/tmp/path0",
		"noTLSVerify": true,
		"disableChunkedEncoding": true,
		"bastionMode": true,
		"proxyAddress": "127.1.2.3",
		"proxyPort": 100,
		"proxyType": "socks5",
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
		]
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
				"connectTimeout": 120,
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
        "enabled": true
    }
}	
`)
	var remoteConfig RemoteConfig
	err = json.Unmarshal(rawConfig, &remoteConfig)
	require.NoError(t, err)
	validate(remoteConfig.Ingress)
}

func TestOriginRequestConfigDefaults(t *testing.T) {
	validate := func(ing Ingress) {
		// Rule 0 didn't override anything, so it inherits the cloudflared defaults
		actual0 := ing.Rules[0].Config
		expected0 := OriginRequestConfig{
			ConnectTimeout:       defaultHTTPConnectTimeout,
			TLSTimeout:           defaultTLSTimeout,
			TCPKeepAlive:         defaultTCPKeepAlive,
			KeepAliveConnections: defaultKeepAliveConnections,
			KeepAliveTimeout:     defaultKeepAliveTimeout,
			ProxyAddress:         defaultProxyAddress,
		}
		require.Equal(t, expected0, actual0)

		// Rule 1 overrode all defaults.
		actual1 := ing.Rules[1].Config
		expected1 := OriginRequestConfig{
			ConnectTimeout:         config.CustomDuration{Duration: 2 * time.Minute},
			TLSTimeout:             config.CustomDuration{Duration: 2 * time.Second},
			TCPKeepAlive:           config.CustomDuration{Duration: 2 * time.Second},
			NoHappyEyeballs:        false,
			KeepAliveTimeout:       config.CustomDuration{Duration: 2 * time.Second},
			KeepAliveConnections:   2,
			HTTPHostHeader:         "def",
			OriginServerName:       "b2",
			CAPool:                 "/tmp/path1",
			NoTLSVerify:            false,
			DisableChunkedEncoding: false,
			BastionMode:            false,
			ProxyAddress:           "interface",
			ProxyPort:              uint(200),
			ProxyType:              "",
			IPRules: []ipaccess.Rule{
				newIPRule(t, "10.0.0.0/16", []int{3000, 3030}, false),
				newIPRule(t, "192.16.0.0/24", []int{5000, 5050}, true),
			},
		}
		require.Equal(t, expected1, actual1)
	}

	rulesYAML := `
ingress:
- hostname: tun.example.com
  service: https://localhost:8000
- hostname: "*"
  service: https://localhost:8001
  originRequest:
    connectTimeout: 2m
    tlsTimeout: 2s
    noHappyEyeballs: false
    tcpKeepAlive: 2s
    keepAliveConnections: 2
    keepAliveTimeout: 2s
    httpHostHeader: def
    originServerName: b2
    caPool: /tmp/path1
    noTLSVerify: false
    disableChunkedEncoding: false
    bastionMode: false
    proxyAddress: interface
    proxyPort: 200
    proxyType: ""
    ipRules:
    - prefix: "10.0.0.0/16"
      ports:
      - 3000
      - 3030
      allow: false
    - prefix: "192.16.0.0/24"
      ports:
      - 5000
      - 5050
      allow: true
`
	ing, err := ParseIngress(MustReadIngress(rulesYAML))
	if err != nil {
		t.Error(err)
	}
	validate(ing)

	rawConfig := []byte(`
{
	"ingress": [
        {
            "hostname": "tun.example.com",
            "service": "https://localhost:8000"
        },
        {
			"hostname": "*",
            "service": "https://localhost:8001",
			"originRequest": {
				"connectTimeout": 120,
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
	]
}
`)

	var remoteConfig RemoteConfig
	err = json.Unmarshal(rawConfig, &remoteConfig)
	require.NoError(t, err)
	validate(remoteConfig.Ingress)
}

func TestDefaultConfigFromCLI(t *testing.T) {
	set := flag.NewFlagSet("contrive", 0)
	c := cli.NewContext(nil, set, nil)

	expected := OriginRequestConfig{
		ConnectTimeout:       defaultHTTPConnectTimeout,
		TLSTimeout:           defaultTLSTimeout,
		TCPKeepAlive:         defaultTCPKeepAlive,
		KeepAliveConnections: defaultKeepAliveConnections,
		KeepAliveTimeout:     defaultKeepAliveTimeout,
		ProxyAddress:         defaultProxyAddress,
	}
	actual := originRequestFromSingeRule(c)
	require.Equal(t, expected, actual)
}

func newIPRule(t *testing.T, prefix string, ports []int, allow bool) ipaccess.Rule {
	rule, err := ipaccess.NewRuleByCIDR(&prefix, ports, allow)
	require.NoError(t, err)
	return rule
}
