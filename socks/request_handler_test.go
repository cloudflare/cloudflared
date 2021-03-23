package socks

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/ipaccess"
)

func TestUnsupportedBind(t *testing.T) {
	req := createRequest(t, socks5Version, bindCommand, "2001:db8::68", 1337, false)
	var b bytes.Buffer

	requestHandler := NewRequestHandler(NewNetDialer(), nil)
	err := requestHandler.Handle(req, &b)
	assert.NoError(t, err)
	assert.True(t, b.Bytes()[1] == commandNotSupported, "expected a response")
}

func TestUnsupportedAssociate(t *testing.T) {
	req := createRequest(t, socks5Version, associateCommand, "127.0.0.1", 1337, false)
	var b bytes.Buffer

	requestHandler := NewRequestHandler(NewNetDialer(), nil)
	err := requestHandler.Handle(req, &b)
	assert.NoError(t, err)
	assert.True(t, b.Bytes()[1] == commandNotSupported, "expected a response")
}

func TestHandleConnect(t *testing.T) {
	req := createRequest(t, socks5Version, connectCommand, "127.0.0.1", 1337, false)
	var b bytes.Buffer

	requestHandler := NewRequestHandler(NewNetDialer(), nil)
	err := requestHandler.Handle(req, &b)
	assert.Error(t, err)
	assert.True(t, b.Bytes()[1] == connectionRefused, "expected a response")
}

func TestHandleConnectIPAccess(t *testing.T) {
	prefix := "127.0.0.0/24"
	rule1, _ := ipaccess.NewRuleByCIDR(&prefix, []int{1337}, true)
	rule2, _ := ipaccess.NewRuleByCIDR(&prefix, []int{1338}, false)
	rules := []ipaccess.Rule{rule1, rule2}
	var b bytes.Buffer

	accessPolicy, _ := ipaccess.NewPolicy(false, nil)
	requestHandler := NewRequestHandler(NewNetDialer(), accessPolicy)
	req := createRequest(t, socks5Version, connectCommand, "127.0.0.1", 1337, false)
	err := requestHandler.Handle(req, &b)
	assert.Error(t, err)
	assert.True(t, b.Bytes()[1] == ruleFailure, "expected to be denied as no rules and defaultAllow=false")

	b.Reset()
	accessPolicy, _ = ipaccess.NewPolicy(true, nil)
	requestHandler = NewRequestHandler(NewNetDialer(), accessPolicy)
	req = createRequest(t, socks5Version, connectCommand, "127.0.0.1", 1337, false)
	err = requestHandler.Handle(req, &b)
	assert.Error(t, err)
	assert.True(t, b.Bytes()[1] == connectionRefused, "expected to be allowed as no rules and defaultAllow=true")

	b.Reset()
	accessPolicy, _ = ipaccess.NewPolicy(false, rules)
	requestHandler = NewRequestHandler(NewNetDialer(), accessPolicy)
	req = createRequest(t, socks5Version, connectCommand, "127.0.0.1", 1337, false)
	err = requestHandler.Handle(req, &b)
	assert.Error(t, err)
	assert.True(t, b.Bytes()[1] == connectionRefused, "expected to be allowed as matching rule")

	b.Reset()
	req = createRequest(t, socks5Version, connectCommand, "127.0.0.1", 1338, false)
	err = requestHandler.Handle(req, &b)
	assert.Error(t, err)
	assert.True(t, b.Bytes()[1] == ruleFailure, "expected to be denied as matching rule")

	b.Reset()
	req = createRequest(t, socks5Version, connectCommand, "127.0.0.1", 1339, false)
	err = requestHandler.Handle(req, &b)
	assert.Error(t, err)
	assert.True(t, b.Bytes()[1] == ruleFailure, "expect to be denied as no matching rule and defaultAllow=false")
}
