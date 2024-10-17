package dnsserver

import (
	"encoding/binary"
	"net"

	"github.com/miekg/dns"
	"github.com/quic-go/quic-go"
)

type DoQWriter struct {
	localAddr  net.Addr
	remoteAddr net.Addr
	stream     quic.Stream
	Msg        *dns.Msg
}

func (w *DoQWriter) Write(b []byte) (int, error) {
	b = AddPrefix(b)
	return w.stream.Write(b)
}

func (w *DoQWriter) WriteMsg(m *dns.Msg) error {
	bytes, err := m.Pack()
	if err != nil {
		return err
	}

	_, err = w.Write(bytes)
	if err != nil {
		return err
	}

	return w.Close()
}

// Close sends the STREAM FIN signal.
// The server MUST send the response(s) on the same stream and MUST
// indicate, after the last response, through the STREAM FIN
// mechanism that no further data will be sent on that stream.
// See https://www.rfc-editor.org/rfc/rfc9250#section-4.2-7
func (w *DoQWriter) Close() error {
	return w.stream.Close()
}

// AddPrefix adds a 2-byte prefix with the DNS message length.
func AddPrefix(b []byte) (m []byte) {
	m = make([]byte, 2+len(b))
	binary.BigEndian.PutUint16(m, uint16(len(b)))
	copy(m[2:], b)

	return m
}

// These methods implement the dns.ResponseWriter interface from Go DNS.
func (w *DoQWriter) TsigStatus() error     { return nil }
func (w *DoQWriter) TsigTimersOnly(b bool) {}
func (w *DoQWriter) Hijack()               {}
func (w *DoQWriter) LocalAddr() net.Addr   { return w.localAddr }
func (w *DoQWriter) RemoteAddr() net.Addr  { return w.remoteAddr }
