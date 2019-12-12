package pogs

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	capnp "zombiezen.com/go/capnproto2"
)

const (
	testURL               = "tunnel.example.com"
	testTunnelID          = "asdfghjkl;"
	testRetryAfterSeconds = 19
)

var (
	testErr         = fmt.Errorf("Invalid credential")
	testLogLines    = []string{"all", "working"}
	testEventDigest = []byte("asdf")
)

// *PermanentRegistrationError implements TunnelRegistrationError
var _ TunnelRegistrationError = (*PermanentRegistrationError)(nil)

// *RetryableRegistrationError implements TunnelRegistrationError
var _ TunnelRegistrationError = (*RetryableRegistrationError)(nil)

func TestTunnelRegistration(t *testing.T) {
	testCases := []*TunnelRegistration{
		NewSuccessfulTunnelRegistration(testURL, testLogLines, testTunnelID, testEventDigest),
		NewSuccessfulTunnelRegistration(testURL, nil, testTunnelID, testEventDigest),
		NewPermanentRegistrationError(testErr).Serialize(),
		NewRetryableRegistrationError(testErr, testRetryAfterSeconds).Serialize(),
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewTunnelRegistration(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalTunnelRegistration(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase #%v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalTunnelRegistration(capnpEntity)
		if !assert.NoError(t, err, "testCase #%v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}

}

func TestConnectResult(t *testing.T) {
	testCases := []ConnectResult{
		&ConnectError{
			Cause:       "it broke",
			ShouldRetry: false,
			RetryAfter:  2 * time.Second,
		},
		&ConnectSuccess{
			ServerLocationName: "SFO",
			Config:             sampleClientConfig(),
		},
		&ConnectSuccess{
			ServerLocationName: "",
			Config:             nil,
		},
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewConnectResult(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalConnectResult(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase #%v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalConnectResult(capnpEntity)
		if !assert.NoError(t, err, "testCase #%v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}

func TestConnectParameters(t *testing.T) {
	testCases := []*ConnectParameters{
		sampleConnectParameters(),
		sampleConnectParameters(func(c *ConnectParameters) {
			c.IntentLabel = "my_intent"
		}),
		sampleConnectParameters(func(c *ConnectParameters) {
			c.Tags = nil
		}),
	}
	for i, testCase := range testCases {
		_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
		capnpEntity, err := tunnelrpc.NewCapnpConnectParameters(seg)
		if !assert.NoError(t, err) {
			t.Fatal("Couldn't initialize a new message")
		}
		err = MarshalConnectParameters(capnpEntity, testCase)
		if !assert.NoError(t, err, "testCase index %v failed to marshal", i) {
			continue
		}
		result, err := UnmarshalConnectParameters(capnpEntity)
		if !assert.NoError(t, err, "testCase index %v failed to unmarshal", i) {
			continue
		}
		assert.Equal(t, testCase, result, "testCase index %v didn't preserve struct through marshalling and unmarshalling", i)
	}
}

func sampleConnectParameters(overrides ...func(*ConnectParameters)) *ConnectParameters {
	cloudflaredID, err := uuid.Parse("ED7BA470-8E54-465E-825C-99712043E01C")
	if err != nil {
		panic(err)
	}
	sample := &ConnectParameters{
		OriginCert:          []byte("my-origin-cert"),
		CloudflaredID:       cloudflaredID,
		NumPreviousAttempts: 19,
		Tags: []Tag{
			Tag{
				Name:  "provision-method",
				Value: "new",
			},
		},
		CloudflaredVersion: "7.0",
		IntentLabel:        "my_intent",
	}
	sample.ensureNoZeroFields()
	for _, f := range overrides {
		f(sample)
	}
	return sample
}

func (c *ConnectParameters) ensureNoZeroFields() {
	ensureNoZeroFieldsInSample(reflect.ValueOf(c), []string{})
}
