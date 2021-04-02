package websocket

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	// example in Sec-Websocket-Key in rfc6455
	testSecWebsocketKey = "dGhlIHNhbXBsZSBub25jZQ=="
	// example Sec-Websocket-Accept in rfc6455
	testSecWebsocketAccept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
)

func TestGenerateAcceptKey(t *testing.T) {
	assert.Equal(t, testSecWebsocketAccept, generateAcceptKey(testSecWebsocketKey))
}
