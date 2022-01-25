package dnsserver

import (
	"net"
	"net/http"

	"github.com/coredns/coredns/plugin/pkg/nonwriter"
)

// DoHWriter is a nonwriter.Writer that adds more specific LocalAddr and RemoteAddr methods.
type DoHWriter struct {
	nonwriter.Writer

	// raddr is the remote's address. This can be optionally set.
	raddr net.Addr
	// laddr is our address. This can be optionally set.
	laddr net.Addr

	// request is the HTTP request we're currently handling.
	request *http.Request
}

// RemoteAddr returns the remote address.
func (d *DoHWriter) RemoteAddr() net.Addr { return d.raddr }

// LocalAddr returns the local address.
func (d *DoHWriter) LocalAddr() net.Addr { return d.laddr }

// Request returns the HTTP request
func (d *DoHWriter) Request() *http.Request { return d.request }
