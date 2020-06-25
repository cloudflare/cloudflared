package socks

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnsupportedBind(t *testing.T) {
	req := createRequest(t, socks5Version, bindCommand, "2001:db8::68", 1337, false)
	var b bytes.Buffer

	requestHandler := NewRequestHandler(NewNetDialer())
	err := requestHandler.Handle(req, &b)
	assert.NoError(t, err)
	assert.True(t, b.Bytes()[1] == commandNotSupported, "expected a response")
}

func TestUnsupportedAssociate(t *testing.T) {
	req := createRequest(t, socks5Version, associateCommand, "127.0.0.1", 1337, false)
	var b bytes.Buffer

	requestHandler := NewRequestHandler(NewNetDialer())
	err := requestHandler.Handle(req, &b)
	assert.NoError(t, err)
	assert.True(t, b.Bytes()[1] == commandNotSupported, "expected a response")
}
