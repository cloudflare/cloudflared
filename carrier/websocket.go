package carrier

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/token"
	"github.com/cloudflare/cloudflared/socks"
	cfwebsocket "github.com/cloudflare/cloudflared/websocket"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// Websocket is used to carry data via WS binary frames over the tunnel from client to the origin
// This implements the functions for glider proxy (sock5) and the carrier interface
type Websocket struct {
	log     *zerolog.Logger
	isSocks bool
}

type wsdialer struct {
	conn *cfwebsocket.GorillaConn
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
func NewWSConnection(log *zerolog.Logger, isSocks bool) Connection {
	return &Websocket{
		log:     log,
		isSocks: isSocks,
	}
}

// ServeStream will create a Websocket client stream connection to the edge
// it blocks and writes the raw data from conn over the tunnel
func (ws *Websocket) ServeStream(options *StartOptions, conn io.ReadWriter) error {
	wsConn, err := createWebsocketStream(options, ws.log)
	if err != nil {
		ws.log.Err(err).Str(LogFieldOriginURL, options.OriginURL).Msg("failed to connect to origin")
		return err
	}
	defer wsConn.Close()

	if ws.isSocks {
		dialer := &wsdialer{conn: wsConn}
		requestHandler := socks.NewRequestHandler(dialer)
		socksServer := socks.NewConnectionHandler(requestHandler)

		_ = socksServer.Serve(conn)
	} else {
		cfwebsocket.Stream(wsConn, conn)
	}
	return nil
}

// StartServer creates a Websocket server to listen for connections.
// This is used on the origin (tunnel) side to take data from the muxer and send it to the origin
func (ws *Websocket) StartServer(listener net.Listener, remote string, shutdownC <-chan struct{}) error {
	return cfwebsocket.StartProxyServer(ws.log, listener, remote, shutdownC, cfwebsocket.DefaultStreamHandler)
}

// createWebsocketStream will create a WebSocket connection to stream data over
// It also handles redirects from Access and will present that flow if
// the token is not present on the request
func createWebsocketStream(options *StartOptions, log *zerolog.Logger) (*cfwebsocket.GorillaConn, error) {
	req, err := http.NewRequest(http.MethodGet, options.OriginURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = options.Headers

	dump, err := httputil.DumpRequest(req, false)
	log.Debug().Msgf("Websocket request: %s", string(dump))

	wsConn, resp, err := cfwebsocket.ClientConnect(req, nil)
	defer closeRespBody(resp)

	if err != nil && IsAccessResponse(resp) {
		wsConn, err = createAccessAuthenticatedStream(options, log)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return &cfwebsocket.GorillaConn{Conn: wsConn}, nil
}

// createAccessAuthenticatedStream will try load a token from storage and make
// a connection with the token set on the request. If it still get redirect,
// this probably means the token in storage is invalid (expired/revoked). If that
// happens it deletes the token and runs the connection again, so the user can
// login again and generate a new one.
func createAccessAuthenticatedStream(options *StartOptions, log *zerolog.Logger) (*websocket.Conn, error) {
	wsConn, resp, err := createAccessWebSocketStream(options, log)
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
	wsConn, resp, err = createAccessWebSocketStream(options, log)
	defer closeRespBody(resp)
	if err != nil {
		return nil, err
	}

	return wsConn, nil
}

// createAccessWebSocketStream builds an Access request and makes a connection
func createAccessWebSocketStream(options *StartOptions, log *zerolog.Logger) (*websocket.Conn, *http.Response, error) {
	req, err := BuildAccessRequest(options, log)
	if err != nil {
		return nil, nil, err
	}

	dump, err := httputil.DumpRequest(req, false)
	log.Debug().Msgf("Access Websocket request: %s", string(dump))

	conn, resp, err := cfwebsocket.ClientConnect(req, nil)

	if resp != nil {
		r, err := httputil.DumpResponse(resp, true)
		if r != nil {
			log.Debug().Msgf("Websocket response: %q", r)
		} else if err != nil {
			log.Debug().Msgf("Websocket response error: %v", err)
		}
	}

	return conn, resp, err
}
