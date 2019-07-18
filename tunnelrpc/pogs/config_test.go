package pogs

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	capnp "zombiezen.com/go/capnproto2"
)

func TestVersion(t *testing.T) {
	firstVersion := InitVersion()
	secondVersion := Version(1)
	assert.False(t, firstVersion.IsNewerOrEqual(secondVersion))
	assert.True(t, secondVersion.IsNewerOrEqual(firstVersion))
	assert.True(t, secondVersion.IsNewerOrEqual(secondVersion))
}

func TestClientConfig(t *testing.T) {
	addDoHProxyConfigs := func(c *ClientConfig) {
		c.DoHProxyConfigs = []*DoHProxyConfig{
			sampleDoHProxyConfig(),
		}
	}
	addReverseProxyConfigs := func(c *ClientConfig) {
		c.ReverseProxyConfigs = []*ReverseProxyConfig{
			sampleReverseProxyConfig(),
			sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
			}),
			sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
				c.Origin = sampleHTTPOriginConfig()
			}),
			sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
				c.Origin = sampleHTTPOriginUnixPathConfig()
			}),
			sampleReverseProxyConfig(func(c *ReverseProxyConfig) {
				c.Origin = sampleWebSocketOriginConfig()
			}),
		}
	}

	testCases := []*ClientConfig{
		sampleClientConfig(),
		sampleClientConfig(addDoHProxyConfigs),
		sampleClientConfig(addReverseProxyConfigs),
		sampleClientConfig(addDoHProxyConfigs, addReverseProxyConfigs),
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewClientConfig(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalClientConfig(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalClientConfig(capnpEntity)
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
			Success: false,
			FailedConfigs: []*FailedConfig{
				{
					Config: sampleReverseProxyConfig(),
					Reason: "Invalid certificate",
				},
				{
					Config: sampleDoHProxyConfig(),
					Reason: "Cannot listen on port 53",
				},
			},
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
			c.Origin = sampleHTTPOriginUnixPathConfig()
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

func TestOriginConfigInvalidURL(t *testing.T) {
	invalidConfigs := []OriginConfig{
		&HTTPOriginConfig{
			// this url doesn't have a scheme
			URLString: "127.0.0.1:36192",
		},
		&WebSocketOriginConfig{
			URLString: "127.0.0.1:36192",
		},
	}

	for _, config := range invalidConfigs {
		service, err := config.Service()
		assert.Error(t, err)
		assert.Nil(t, service)
	}
}

//////////////////////////////////////////////////////////////////////////////
// Functions to generate sample data for ease of testing

func sampleClientConfig(overrides ...func(*ClientConfig)) *ClientConfig {
	sample := &ClientConfig{
		Version: Version(1337),
		SupervisorConfig: &SupervisorConfig{
			AutoUpdateFrequency:    21 * time.Hour,
			MetricsUpdateFrequency: 11 * time.Minute,
			GracePeriod:            31 * time.Second,
		},
		EdgeConnectionConfig: &EdgeConnectionConfig{
			NumHAConnections:    49,
			Timeout:             9 * time.Second,
			HeartbeatInterval:   5 * time.Second,
			MaxFailedHeartbeats: 9001,
		},
	}
	sample.ensureNoZeroFields()
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
	sample.ensureNoZeroFields()
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func sampleReverseProxyConfig(overrides ...func(*ReverseProxyConfig)) *ReverseProxyConfig {
	sample := &ReverseProxyConfig{
		TunnelHostname:     "hijk.example.com",
		Origin:             &HelloWorldOriginConfig{},
		Retries:            18,
		ConnectionTimeout:  5 * time.Second,
		CompressionQuality: 4,
	}
	sample.ensureNoZeroFields()
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func sampleHTTPOriginConfig(overrides ...func(*HTTPOriginConfig)) *HTTPOriginConfig {
	sample := &HTTPOriginConfig{
		URLString:              "https.example.com",
		TCPKeepAlive:           7 * time.Second,
		DialDualStack:          true,
		TLSHandshakeTimeout:    11 * time.Second,
		TLSVerify:              true,
		OriginCAPool:           "/etc/cert.pem",
		OriginServerName:       "secure.example.com",
		MaxIdleConnections:     19,
		IdleConnectionTimeout:  17 * time.Second,
		ProxyConnectionTimeout: 15 * time.Second,
		ExpectContinueTimeout:  21 * time.Second,
		ChunkedEncoding:        true,
	}
	sample.ensureNoZeroFields()
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func sampleHTTPOriginUnixPathConfig(overrides ...func(*HTTPOriginConfig)) *HTTPOriginConfig {
	sample := &HTTPOriginConfig{
		URLString:              "unix:/var/lib/file.sock",
		TCPKeepAlive:           7 * time.Second,
		DialDualStack:          true,
		TLSHandshakeTimeout:    11 * time.Second,
		TLSVerify:              true,
		OriginCAPool:           "/etc/cert.pem",
		OriginServerName:       "secure.example.com",
		MaxIdleConnections:     19,
		IdleConnectionTimeout:  17 * time.Second,
		ProxyConnectionTimeout: 15 * time.Second,
		ExpectContinueTimeout:  21 * time.Second,
		ChunkedEncoding:        true,
	}
	sample.ensureNoZeroFields()
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func sampleWebSocketOriginConfig(overrides ...func(*WebSocketOriginConfig)) *WebSocketOriginConfig {
	sample := &WebSocketOriginConfig{
		URLString:        "ssh://example.com",
		TLSVerify:        true,
		OriginCAPool:     "/etc/cert.pem",
		OriginServerName: "secure.example.com",
	}
	sample.ensureNoZeroFields()
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func (c *ClientConfig) ensureNoZeroFields() {
	ensureNoZeroFieldsInSample(reflect.ValueOf(c), []string{"DoHProxyConfigs", "ReverseProxyConfigs"})
}

func (c *DoHProxyConfig) ensureNoZeroFields() {
	ensureNoZeroFieldsInSample(reflect.ValueOf(c), []string{})
}

func (c *ReverseProxyConfig) ensureNoZeroFields() {
	ensureNoZeroFieldsInSample(reflect.ValueOf(c), []string{})
}

func (c *HTTPOriginConfig) ensureNoZeroFields() {
	ensureNoZeroFieldsInSample(reflect.ValueOf(c), []string{})
}

func (c *WebSocketOriginConfig) ensureNoZeroFields() {
	ensureNoZeroFieldsInSample(reflect.ValueOf(c), []string{})
}

// ensureNoZeroFieldsInSample checks that all fields in the sample struct,
// except those listed in `allowedZeroFieldNames`, are initialized to nonzero
// values. Note that the value has to be a pointer for reflection to work
// correctly:
//     e := &ExampleStruct{ ... }
//     ensureNoZeroFieldsInSample(reflect.ValueOf(e), []string{})
//
// Context:
// Our tests work by taking a sample struct and marshalling/unmarshalling it.
// This makes them easy to write, but introduces some risk: if we don't
// include a field in the sample value, it won't be covered under tests.
// This check reduces that risk by requiring fields to be either initialized
// or explicitly uninitialized.
// https://bitbucket.cfdata.org/projects/TUN/repos/cloudflared/pull-requests/151/overview?commentId=348012
func ensureNoZeroFieldsInSample(ptrToSampleValue reflect.Value, allowedZeroFieldNames []string) {
	sampleValue := ptrToSampleValue.Elem()
	structType := ptrToSampleValue.Type().Elem()

	allowedZeroFieldSet := make(map[string]bool)
	for _, name := range allowedZeroFieldNames {
		if _, ok := structType.FieldByName(name); !ok {
			panic(fmt.Sprintf("struct %v has no field %v", structType.Name(), name))
		}
		allowedZeroFieldSet[name] = true
	}

	for i := 0; i < structType.NumField(); i++ {
		if allowedZeroFieldSet[structType.Field(i).Name] {
			continue
		}

		zeroValue := reflect.Zero(structType.Field(i).Type)
		if reflect.DeepEqual(zeroValue.Interface(), sampleValue.Field(i).Interface()) {
			panic(fmt.Sprintf("In the sample value for struct %v, field %v was not initialized", structType.Name(), structType.Field(i).Name))
		}
	}
}
