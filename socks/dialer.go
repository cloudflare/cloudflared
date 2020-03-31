package socks

import (
	"fmt"
	"io"
	"net"
)

// Dialer is used to provided the transport of the proxy
type Dialer interface {
	Dial(string) (io.ReadWriteCloser, *AddrSpec, error)
}

// NetDialer is a standard TCP dialer
type NetDialer struct {
}

// NewNetDialer creates a new dialer
func NewNetDialer() Dialer {
	return &NetDialer{}
}

// Dial is a base TCP dialer
func (d *NetDialer) Dial(address string) (io.ReadWriteCloser, *AddrSpec, error) {
	c, err := net.Dial("tcp", address)
	if err != nil {
		return nil, nil, err
	}

	local := c.LocalAddr().(*net.TCPAddr)
	addr := AddrSpec{IP: local.IP, Port: local.Port}

	return c, &addr, nil
}

// ConnDialer is like NetDialer but with an existing TCP dialer already created
type ConnDialer struct {
	conn net.Conn
}

// NewConnDialer creates a new dialer with a already created net.conn (TCP expected)
func NewConnDialer(conn net.Conn) Dialer {
	return &ConnDialer{
		conn: conn,
	}
}

// Dial is a TCP dialer but already created
func (d *ConnDialer) Dial(address string) (io.ReadWriteCloser, *AddrSpec, error) {
	local, ok := d.conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		return nil, nil, fmt.Errorf("not a tcp connection")
	}

	addr := AddrSpec{IP: local.IP, Port: local.Port}
	return d.conn, &addr, nil
}
