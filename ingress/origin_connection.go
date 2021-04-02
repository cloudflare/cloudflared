package ingress

import (
	"context"
	"io"
	"net"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/ipaccess"
	"github.com/cloudflare/cloudflared/socks"
	"github.com/cloudflare/cloudflared/websocket"
)

// OriginConnection is a way to stream to a service running on the user's origin.
// Different concrete implementations will stream different protocols as long as they are io.ReadWriters.
type OriginConnection interface {
	// Stream should generally be implemented as a bidirectional io.Copy.
	Stream(ctx context.Context, tunnelConn io.ReadWriter, log *zerolog.Logger)
	Close()
}

type streamHandlerFunc func(originConn io.ReadWriter, remoteConn net.Conn, log *zerolog.Logger)

// DefaultStreamHandler is an implementation of streamHandlerFunc that
// performs a two way io.Copy between originConn and remoteConn.
func DefaultStreamHandler(originConn io.ReadWriter, remoteConn net.Conn, log *zerolog.Logger) {
	websocket.Stream(originConn, remoteConn, log)
}

// tcpConnection is an OriginConnection that directly streams to raw TCP.
type tcpConnection struct {
	conn net.Conn
}

func (tc *tcpConnection) Stream(ctx context.Context, tunnelConn io.ReadWriter, log *zerolog.Logger) {
	websocket.Stream(tunnelConn, tc.conn, log)
}

func (tc *tcpConnection) Close() {
	tc.conn.Close()
}

// tcpOverWSConnection is an OriginConnection that streams to TCP over WS.
type tcpOverWSConnection struct {
	conn          net.Conn
	streamHandler streamHandlerFunc
}

func (wc *tcpOverWSConnection) Stream(ctx context.Context, tunnelConn io.ReadWriter, log *zerolog.Logger) {
	wc.streamHandler(websocket.NewConn(ctx, tunnelConn, log), wc.conn, log)
}

func (wc *tcpOverWSConnection) Close() {
	wc.conn.Close()
}

// socksProxyOverWSConnection is an OriginConnection that streams SOCKS connections over WS.
// The connection to the origin happens inside the SOCKS code as the client specifies the origin
// details in the packet.
type socksProxyOverWSConnection struct {
	accessPolicy *ipaccess.Policy
}

func (sp *socksProxyOverWSConnection) Stream(ctx context.Context, tunnelConn io.ReadWriter, log *zerolog.Logger) {
	socks.StreamNetHandler(websocket.NewConn(ctx, tunnelConn, log), sp.accessPolicy, log)
}

func (sp *socksProxyOverWSConnection) Close() {
}

// wsProxyConnection represents a bidirectional stream for a websocket connection to the origin
type wsProxyConnection struct {
	rwc io.ReadWriteCloser
}

func (conn *wsProxyConnection) Stream(ctx context.Context, tunnelConn io.ReadWriter, log *zerolog.Logger) {
	websocket.Stream(tunnelConn, conn.rwc, log)
}

func (conn *wsProxyConnection) Close() {
	conn.rwc.Close()
}
