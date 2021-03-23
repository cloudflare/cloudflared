package ingress

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	yaml "gopkg.in/yaml.v2"

	"github.com/cloudflare/cloudflared/config"
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

func TestOriginRequestConfigOverrides(t *testing.T) {
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
`
	ing, err := ParseIngress(MustReadIngress(rulesYAML))
	if err != nil {
		t.Error(err)
	}

	// Rule 0 didn't override anything, so it inherits the user-specified
	// root-level configuration.
	actual0 := ing.Rules[0].Config
	expected0 := OriginRequestConfig{
		ConnectTimeout:         1 * time.Minute,
		TLSTimeout:             1 * time.Second,
		NoHappyEyeballs:        true,
		TCPKeepAlive:           1 * time.Second,
		KeepAliveConnections:   1,
		KeepAliveTimeout:       1 * time.Second,
		HTTPHostHeader:         "abc",
		OriginServerName:       "a1",
		CAPool:                 "/tmp/path0",
		NoTLSVerify:            true,
		DisableChunkedEncoding: true,
		BastionMode:            true,
		ProxyAddress:           "127.1.2.3",
		ProxyPort:              uint(100),
		ProxyType:              "socks5",
	}
	require.Equal(t, expected0, actual0)

	// Rule 1 overrode all the root-level config.
	actual1 := ing.Rules[1].Config
	expected1 := OriginRequestConfig{
		ConnectTimeout:         2 * time.Minute,
		TLSTimeout:             2 * time.Second,
		NoHappyEyeballs:        false,
		TCPKeepAlive:           2 * time.Second,
		KeepAliveConnections:   2,
		KeepAliveTimeout:       2 * time.Second,
		HTTPHostHeader:         "def",
		OriginServerName:       "b2",
		CAPool:                 "/tmp/path1",
		NoTLSVerify:            false,
		DisableChunkedEncoding: false,
		BastionMode:            false,
		ProxyAddress:           "interface",
		ProxyPort:              uint(200),
		ProxyType:              "",
	}
	require.Equal(t, expected1, actual1)
}

func TestOriginRequestConfigDefaults(t *testing.T) {
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
`
	ing, err := ParseIngress(MustReadIngress(rulesYAML))
	if err != nil {
		t.Error(err)
	}

	// Rule 0 didn't override anything, so it inherits the cloudflared defaults
	actual0 := ing.Rules[0].Config
	expected0 := OriginRequestConfig{
		ConnectTimeout:       defaultConnectTimeout,
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
		ConnectTimeout:         2 * time.Minute,
		TLSTimeout:             2 * time.Second,
		NoHappyEyeballs:        false,
		TCPKeepAlive:           2 * time.Second,
		KeepAliveConnections:   2,
		KeepAliveTimeout:       2 * time.Second,
		HTTPHostHeader:         "def",
		OriginServerName:       "b2",
		CAPool:                 "/tmp/path1",
		NoTLSVerify:            false,
		DisableChunkedEncoding: false,
		BastionMode:            false,
		ProxyAddress:           "interface",
		ProxyPort:              uint(200),
		ProxyType:              "",
	}
	require.Equal(t, expected1, actual1)
}

func TestDefaultConfigFromCLI(t *testing.T) {
	set := flag.NewFlagSet("contrive", 0)
	c := cli.NewContext(nil, set, nil)

	expected := OriginRequestConfig{
		ConnectTimeout:       defaultConnectTimeout,
		TLSTimeout:           defaultTLSTimeout,
		TCPKeepAlive:         defaultTCPKeepAlive,
		KeepAliveConnections: defaultKeepAliveConnections,
		KeepAliveTimeout:     defaultKeepAliveTimeout,
		ProxyAddress:         defaultProxyAddress,
	}
	actual := originRequestFromSingeRule(c)
	require.Equal(t, expected, actual)
}
