package pogs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	capnp "zombiezen.com/go/capnproto2"
)

func TestCloudflaredConfig(t *testing.T) {
	addDoHProxyConfigs := func(c *CloudflaredConfig) {
		c.DoHProxyConfigs = []*DoHProxyConfig{
			sampleDoHProxyConfig(),
		}
	}
	addReverseProxyConfigs := func(c *CloudflaredConfig) {
		c.ReverseProxyConfigs = []*ReverseProxyConfig{
			sampleReverseProxyConfig(),
			sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
				c.Origin = sampleHTTPOriginConfig()
			}),
			sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
				c.Origin = sampleUnixSocketOriginConfig()
			}),
			sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
				c.Origin = sampleWebSocketOriginConfig()
			}),
		}
	}

	testCases := []*CloudflaredConfig{
		sampleCloudflaredConfig(),
		sampleCloudflaredConfig(addDoHProxyConfigs),
		sampleCloudflaredConfig(addReverseProxyConfigs),
		sampleCloudflaredConfig(addDoHProxyConfigs, addReverseProxyConfigs),
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewCloudflaredConfig(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalCloudflaredConfig(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalCloudflaredConfig(capnpEntity)
		if !assert.NoError(t, err, "testCase index %v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}

func TestUseConfigurationResult(t *testing.T) {
	testCases := []*UseConfigurationResult{
		&UseConfigurationResult{
			Success: true,
		},
		&UseConfigurationResult{
			Success:      false,
			ErrorMessage: "the quick brown fox jumped over the lazy dogs",
		},
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewUseConfigurationResult(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalUseConfigurationResult(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalUseConfigurationResult(capnpEntity)
		if !assert.NoError(t, err, "testCase index %v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}

func TestDoHProxyConfig(t *testing.T) {
	testCases := []*DoHProxyConfig{
		sampleDoHProxyConfig(),
		sampleDoHProxyConfig(func(c *DoHProxyConfig) {
			c.Upstreams = nil
		}),
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewDoHProxyConfig(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalDoHProxyConfig(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalDoHProxyConfig(capnpEntity)
		if !assert.NoError(t, err, "testCase index %v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}

func TestReverseProxyConfig(t *testing.T) {
	testCases := []*ReverseProxyConfig{
		sampleReverseProxyConfig(),
		sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
			c.Origin = sampleHTTPOriginConfig()
		}),
		sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
			c.Origin = sampleUnixSocketOriginConfig()
		}),
		sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
			c.Origin = sampleWebSocketOriginConfig()
		}),
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewReverseProxyConfig(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalReverseProxyConfig(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalReverseProxyConfig(capnpEntity)
		if !assert.NoError(t, err, "testCase index %v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}

func TestHTTPOriginConfig(t *testing.T) {
	testCases := []*HTTPOriginConfig{
		sampleHTTPOriginConfig(),
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewHTTPOriginConfig(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalHTTPOriginConfig(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalHTTPOriginConfig(capnpEntity)
		if !assert.NoError(t, err, "testCase index %v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}

func TestUnixSocketOriginConfig(t *testing.T) {
	testCases := []*UnixSocketOriginConfig{
		sampleUnixSocketOriginConfig(),
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewUnixSocketOriginConfig(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalUnixSocketOriginConfig(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalUnixSocketOriginConfig(capnpEntity)
		if !assert.NoError(t, err, "testCase index %v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}

func TestWebSocketOriginConfig(t *testing.T) {
	testCases := []*WebSocketOriginConfig{
		sampleWebSocketOriginConfig(),
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewWebSocketOriginConfig(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalWebSocketOriginConfig(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalWebSocketOriginConfig(capnpEntity)
		if !assert.NoError(t, err, "testCase index %v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}

//////////////////////////////////////////////////////////////////////////////
// Functions to generate sample data for ease of testing

func sampleCloudflaredConfig(overrides ...func(*CloudflaredConfig)) *CloudflaredConfig {
	// strip the location and monotonic clock reading so that assert.Equals()
	// will work correctly
	now := time.Now().UTC().Round(0)
	sample := &CloudflaredConfig{
		Timestamp:              now,
		AutoUpdateFrequency:    21 * time.Hour,
		MetricsUpdateFrequency: 11 * time.Minute,
		HeartbeatInterval:      5 * time.Second,
		MaxFailedHeartbeats:    9001,
		GracePeriod:            31 * time.Second,
	}
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func sampleDoHProxyConfig(overrides ...func(*DoHProxyConfig)) *DoHProxyConfig {
	sample := &DoHProxyConfig{
		ListenHost: "127.0.0.1",
		ListenPort: 53,
		Upstreams:  []string{"https://1.example.com", "https://2.example.com"},
	}
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func sampleReverseProxyConfig(overrides ...func(*ReverseProxyConfig)) *ReverseProxyConfig {
	sample := &ReverseProxyConfig{
		TunnelID:           "hijk",
		Origin:             &HelloWorldOriginConfig{},
		Retries:            18,
		ConnectionTimeout:  5 * time.Second,
		ChunkedEncoding:    false,
		CompressionQuality: 4,
	}
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func sampleHTTPOriginConfig(overrides ...func(*HTTPOriginConfig)) *HTTPOriginConfig {
	sample := &HTTPOriginConfig{
		URL:                   "https://example.com",
		TCPKeepAlive:          7 * time.Second,
		DialDualStack:         true,
		TLSHandshakeTimeout:   11 * time.Second,
		TLSVerify:             true,
		OriginCAPool:          "/etc/cert.pem",
		OriginServerName:      "secure.example.com",
		MaxIdleConnections:    19,
		IdleConnectionTimeout: 17 * time.Second,
	}
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func sampleUnixSocketOriginConfig(overrides ...func(*UnixSocketOriginConfig)) *UnixSocketOriginConfig {
	sample := &UnixSocketOriginConfig{
		Path: "/var/lib/file.sock",
	}
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func sampleWebSocketOriginConfig(overrides ...func(*WebSocketOriginConfig)) *WebSocketOriginConfig {
	sample := &WebSocketOriginConfig{
		URL: "ssh://example.com",
	}
	for _, f := range overrides {
		f(sample)
	}
	return sample
}
