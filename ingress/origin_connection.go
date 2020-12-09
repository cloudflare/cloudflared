package ingress

import (
	"io"
	"net"
	"net/http"

	"github.com/cloudflare/cloudflared/websocket"
	gws "github.com/gorilla/websocket"
)

// OriginConnection is a way to stream to a service running on the user's origin.
// Different concrete implementations will stream different protocols as long as they are io.ReadWriters.
type OriginConnection interface {
	// Stream should generally be implemented as a bidirectional io.Copy.
	Stream(tunnelConn io.ReadWriter)
	Close()
}

// tcpConnection is an OriginConnection that directly streams to raw TCP.
type tcpConnection struct {
	conn          net.Conn
	streamHandler func(tunnelConn io.ReadWriter, originConn net.Conn)
}

func (tc *tcpConnection) Stream(tunnelConn io.ReadWriter) {
	tc.streamHandler(tunnelConn, tc.conn)
}

func (tc *tcpConnection) Close() {
	tc.conn.Close()
}

// wsConnection is an OriginConnection that streams to TCP packets by encapsulating them in Websockets.
// TODO: TUN-3710 Remove wsConnection and have helloworld service reuse tcpConnection like bridgeService does.
type wsConnection struct {
	wsConn *gws.Conn
	resp   *http.Response
}

func (wsc *wsConnection) Stream(tunnelConn io.ReadWriter) {
	websocket.Stream(tunnelConn, wsc.wsConn.UnderlyingConn())
}

func (wsc *wsConnection) Close() {
	wsc.resp.Body.Close()
	wsc.wsConn.Close()
}

func newWSConnection(transport *http.Transport, r *http.Request) (OriginConnection, error) {
	d := &gws.Dialer{
		TLSClientConfig: transport.TLSClientConfig,
	}
	wsConn, resp, err := websocket.ClientConnect(r, d)
	if err != nil {
		return nil, err
	}
	return &wsConnection{
		wsConn,
		resp,
	}, nil
}
