package socks

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func createRequestData(version, command uint8, ip net.IP, port uint16) []byte {
	// set the command
	b := []byte{version, command, 0}

	// append the ip
	if len(ip) == net.IPv4len {
		b = append(b, 1)
		b = append(b, ip.To4()...)
	} else {
		b = append(b, 4)
		b = append(b, ip.To16()...)
	}

	// append the port
	p := []byte{0, 0}
	binary.BigEndian.PutUint16(p, port)
	b = append(b, p...)

	return b
}

func createRequest(t *testing.T, version, command uint8, ipStr string, port uint16, shouldFail bool) *Request {
	ip := net.ParseIP(ipStr)
	data := createRequestData(version, command, ip, port)
	reader := bytes.NewReader(data)
	req, err := NewRequest(reader)
	if shouldFail {
		assert.Error(t, err)
		return nil
	}
	assert.NoError(t, err)
	assert.True(t, req.Version == socks5Version, "version doesn't match expectation: %v", req.Version)
	assert.True(t, req.Command == command, "command doesn't match expectation: %v", req.Command)
	assert.True(t, req.DestAddr.Port == int(port), "port doesn't match expectation: %v", req.DestAddr.Port)
	assert.True(t, req.DestAddr.IP.String() == ipStr, "ip doesn't match expectation: %v", req.DestAddr.IP.String())

	return req
}

func TestValidConnectRequest(t *testing.T) {
	createRequest(t, socks5Version, connectCommand, "127.0.0.1", 1337, false)
}

func TestValidBindRequest(t *testing.T) {
	createRequest(t, socks5Version, bindCommand, "2001:db8::68", 1337, false)
}

func TestValidAssociateRequest(t *testing.T) {
	createRequest(t, socks5Version, associateCommand, "127.0.0.1", 1234, false)
}

func TestInValidVersionRequest(t *testing.T) {
	createRequest(t, 4, connectCommand, "127.0.0.1", 1337, true)
}

func TestInValidIPRequest(t *testing.T) {
	createRequest(t, 4, connectCommand, "127.0.01", 1337, true)
}
