package carrier

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/token"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/socks"
	cfwebsocket "github.com/cloudflare/cloudflared/websocket"
	"github.com/gorilla/websocket"
)

// Websocket is used to carry data via WS binary frames over the tunnel from client to the origin
// This implements the functions for glider proxy (sock5) and the carrier interface
type Websocket struct {
	logger  logger.Service
	isSocks bool
}

type wsdialer struct {
	conn *cfwebsocket.Conn
}

func (d *wsdialer) Dial(address string) (io.ReadWriteCloser, *socks.AddrSpec, error) {
	local, ok := d.conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		return nil, nil, fmt.Errorf("not a tcp connection")
	}

	addr := socks.AddrSpec{IP: local.IP, Port: local.Port}
	return d.conn, &addr, nil
}

// NewWSConnection returns a new connection object
func NewWSConnection(logger logger.Service, isSocks bool) Connection {
	return &Websocket{
		logger:  logger,
		isSocks: isSocks,
	}
}

// ServeStream will create a Websocket client stream connection to the edge
// it blocks and writes the raw data from conn over the tunnel
func (ws *Websocket) ServeStream(options *StartOptions, conn io.ReadWriter) error {
	wsConn, err := createWebsocketStream(options, ws.logger)
	if err != nil {
		ws.logger.Errorf("failed to connect to %s with error: %s", options.OriginURL, err)
		return err
	}
	defer wsConn.Close()

	if ws.isSocks {
		dialer := &wsdialer{conn: wsConn}
		requestHandler := socks.NewRequestHandler(dialer)
		socksServer := socks.NewConnectionHandler(requestHandler)

		socksServer.Serve(conn)
	} else {
		cfwebsocket.Stream(wsConn, conn)
	}
	return nil
}

// StartServer creates a Websocket server to listen for connections.
// This is used on the origin (tunnel) side to take data from the muxer and send it to the origin
func (ws *Websocket) StartServer(listener net.Listener, remote string, shutdownC <-chan struct{}) error {
	return cfwebsocket.StartProxyServer(ws.logger, listener, remote, shutdownC, cfwebsocket.DefaultStreamHandler)
}

// createWebsocketStream will create a WebSocket connection to stream data over
// It also handles redirects from Access and will present that flow if
// the token is not present on the request
func createWebsocketStream(options *StartOptions, logger logger.Service) (*cfwebsocket.Conn, error) {
	req, err := http.NewRequest(http.MethodGet, options.OriginURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = options.Headers

	dump, err := httputil.DumpRequest(req, false)
	logger.Debugf("Websocket request: %s", string(dump))

	wsConn, resp, err := cfwebsocket.ClientConnect(req, nil)
	defer closeRespBody(resp)
	if err != nil && IsAccessResponse(resp) {
		wsConn, err = createAccessAuthenticatedStream(options, logger)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return &cfwebsocket.Conn{Conn: wsConn}, nil
}

// createAccessAuthenticatedStream will try load a token from storage and make
// a connection with the token set on the request. If it still get redirect,
// this probably means the token in storage is invalid (expired/revoked). If that
// happens it deletes the token and runs the connection again, so the user can
// login again and generate a new one.
func createAccessAuthenticatedStream(options *StartOptions, logger logger.Service) (*websocket.Conn, error) {
	wsConn, resp, err := createAccessWebSocketStream(options, logger)
	defer closeRespBody(resp)
	if err == nil {
		return wsConn, nil
	}

	if !IsAccessResponse(resp) {
		return nil, err
	}

	// Access Token is invalid for some reason. Go through regen flow
	originReq, err := http.NewRequest(http.MethodGet, options.OriginURL, nil)
	if err != nil {
		return nil, err
	}
	if err := token.RemoveTokenIfExists(originReq.URL); err != nil {
		return nil, err
	}
	wsConn, resp, err = createAccessWebSocketStream(options, logger)
	defer closeRespBody(resp)
	if err != nil {
		return nil, err
	}

	return wsConn, nil
}

// createAccessWebSocketStream builds an Access request and makes a connection
func createAccessWebSocketStream(options *StartOptions, logger logger.Service) (*websocket.Conn, *http.Response, error) {
	req, err := BuildAccessRequest(options, logger)
	if err != nil {
		return nil, nil, err
	}

	dump, err := httputil.DumpRequest(req, false)
	logger.Debugf("Access Websocket request: %s", string(dump))

	return cfwebsocket.ClientConnect(req, nil)
}
