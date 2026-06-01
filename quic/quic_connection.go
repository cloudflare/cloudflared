package quic

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/quic-go/quic-go"
)

// QUICConnection defines the subset of [quic.Connection] methods used by cloudflared.
// Consumers should accept this interface; producers should return [*ConnWithCloser].
type QUICConnection interface {
	AcceptStream(ctx context.Context) (quic.Stream, error)
	OpenStream() (quic.Stream, error)
	OpenStreamSync(ctx context.Context) (quic.Stream, error)
	CloseWithError(code quic.ApplicationErrorCode, reason string) error
	Context() context.Context
	SendDatagram(payload []byte) error
	ReceiveDatagram(ctx context.Context) ([]byte, error)
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	ConnectionState() quic.ConnectionState
}

// Compile-time assertion that *ConnWithCloser implements QUICConnection.
var _ QUICConnection = (*ConnWithCloser)(nil)

var (
	// error returned when the [NewConnWithCloser] is called with a nil conn argument
	ErrNilQuicConnection = errors.New("the provided quic connection is nil")
	// error returned when the [NewConnWithCloser] is called with a nil closer argument
	ErrNilCloser = errors.New("the provided closer is nil")
)

// ConnWithCloser wraps a [quic.Connection] and an [io.Closer] (typically the
// underlying [*net.UDPConn]). When [CloseWithError] is called the QUIC
// connection is closed first, then the closer is closed deterministically.
//
// A nil conn is only safe for [CloseWithError] (used in tests). All other
// delegated methods will panic on a nil conn.
type ConnWithCloser struct {
	conn   quic.Connection
	closer io.Closer
}

// NewQUICConnection returns a [*ConnWithCloser] that will close closer after
// the QUIC connection is closed.
func NewQUICConnection(conn quic.Connection, closer io.Closer) (*ConnWithCloser, error) {
	if conn == nil {
		return nil, ErrNilQuicConnection
	}

	if closer == nil {
		return nil, ErrNilCloser
	}
	return &ConnWithCloser{conn: conn, closer: closer}, nil
}

// CloseWithError closes the QUIC connection and then closes the underlying
// [io.Closer]. If both operations return errors, the errors are joined so that
// the closer error is no longer silently discarded.
func (c *ConnWithCloser) CloseWithError(code quic.ApplicationErrorCode, reason string) error {
	connErr := c.conn.CloseWithError(code, reason)
	closerErr := c.closer.Close()

	return errors.Join(connErr, closerErr)
}

func (c *ConnWithCloser) AcceptStream(ctx context.Context) (quic.Stream, error) {
	return c.conn.AcceptStream(ctx)
}

func (c *ConnWithCloser) OpenStream() (quic.Stream, error) {
	return c.conn.OpenStream()
}

func (c *ConnWithCloser) OpenStreamSync(ctx context.Context) (quic.Stream, error) {
	return c.conn.OpenStreamSync(ctx)
}

func (c *ConnWithCloser) Context() context.Context {
	return c.conn.Context()
}

func (c *ConnWithCloser) SendDatagram(payload []byte) error {
	return c.conn.SendDatagram(payload)
}

func (c *ConnWithCloser) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	return c.conn.ReceiveDatagram(ctx)
}

func (c *ConnWithCloser) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *ConnWithCloser) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *ConnWithCloser) ConnectionState() quic.ConnectionState {
	return c.conn.ConnectionState()
}
