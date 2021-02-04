package ingress

import (
	"io"
	"net"
	"net/http"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/websocket"
	gws "github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// OriginConnection is a way to stream to a service running on the user's origin.
// Different concrete implementations will stream different protocols as long as they are io.ReadWriters.
type OriginConnection interface {
	// Stream should generally be implemented as a bidirectional io.Copy.
	Stream(tunnelConn io.ReadWriter, log *zerolog.Logger)
	Close()
	Type() connection.Type
}

type streamHandlerFunc func(originConn io.ReadWriter, remoteConn net.Conn, log *zerolog.Logger)

// Stream copies copy data to & from provided io.ReadWriters.
func Stream(conn, backendConn io.ReadWriter, log *zerolog.Logger) {
	proxyDone := make(chan struct{}, 2)

	go func() {
		_, err := io.Copy(conn, backendConn)
		if err != nil {
			log.Debug().Msgf("conn to backendConn copy: %v", err)
		}
		proxyDone <- struct{}{}
	}()

	go func() {
		_, err := io.Copy(backendConn, conn)
		if err != nil {
			log.Debug().Msgf("backendConn to conn copy: %v", err)
		}
		proxyDone <- struct{}{}
	}()

	// If one side is done, we are done.
	<-proxyDone
}

// DefaultStreamHandler is an implementation of streamHandlerFunc that
// performs a two way io.Copy between originConn and remoteConn.
func DefaultStreamHandler(originConn io.ReadWriter, remoteConn net.Conn, log *zerolog.Logger) {
	Stream(originConn, remoteConn, log)
}

// tcpConnection is an OriginConnection that directly streams to raw TCP.
type tcpConnection struct {
	conn          net.Conn
	streamHandler streamHandlerFunc
}

func (tc *tcpConnection) Stream(tunnelConn io.ReadWriter, log *zerolog.Logger) {
	tc.streamHandler(tunnelConn, tc.conn, log)
}

func (tc *tcpConnection) Close() {
	tc.conn.Close()
}

func (*tcpConnection) Type() connection.Type {
	return connection.TypeTCP
}

// wsConnection is an OriginConnection that streams to TCP packets by encapsulating them in Websockets.
// TODO: TUN-3710 Remove wsConnection and have helloworld service reuse tcpConnection like bridgeService does.
type wsConnection struct {
	wsConn *gws.Conn
	resp   *http.Response
}

func (wsc *wsConnection) Stream(tunnelConn io.ReadWriter, log *zerolog.Logger) {
	Stream(tunnelConn, wsc.wsConn.UnderlyingConn(), log)
}

func (wsc *wsConnection) Close() {
	wsc.resp.Body.Close()
	wsc.wsConn.Close()
}

func (wsc *wsConnection) Type() connection.Type {
	return connection.TypeWebsocket
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
