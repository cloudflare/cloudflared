package pogs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	capnp "zombiezen.com/go/capnproto2"

	"github.com/cloudflare/cloudflared/tunnelrpc"
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
	testConnDigest  = []byte("lkjh")
)

// *PermanentRegistrationError implements TunnelRegistrationError
var _ TunnelRegistrationError = (*PermanentRegistrationError)(nil)

// *RetryableRegistrationError implements TunnelRegistrationError
var _ TunnelRegistrationError = (*RetryableRegistrationError)(nil)

func TestTunnelRegistration(t *testing.T) {
	testCases := []*TunnelRegistration{
		NewSuccessfulTunnelRegistration(testURL, testLogLines, testTunnelID, testEventDigest, testConnDigest),
		NewSuccessfulTunnelRegistration(testURL, nil, testTunnelID, testEventDigest, testConnDigest),
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
