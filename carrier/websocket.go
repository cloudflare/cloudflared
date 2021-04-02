package carrier

import (
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/token"
	cfwebsocket "github.com/cloudflare/cloudflared/websocket"
)

// Websocket is used to carry data via WS binary frames over the tunnel from client to the origin
// This implements the functions for glider proxy (sock5) and the carrier interface
type Websocket struct {
	log     *zerolog.Logger
	isSocks bool
}

// NewWSConnection returns a new connection object
func NewWSConnection(log *zerolog.Logger) Connection {
	return &Websocket{
		log: log,
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

	cfwebsocket.Stream(wsConn, conn, ws.log)
	return nil
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
	if options.Host != "" {
		req.Host = options.Host
	}

	dump, err := httputil.DumpRequest(req, false)
	log.Debug().Msgf("Websocket request: %s", string(dump))

	dialer := &websocket.Dialer{
		TLSClientConfig: options.TLSClientConfig,
		Proxy:           http.ProxyFromEnvironment,
	}
	wsConn, resp, err := clientConnect(req, dialer)
	defer closeRespBody(resp)

	if err != nil && IsAccessResponse(resp) {
		// Only get Access app info if we know the origin is protected by Access
		originReq, err := http.NewRequest(http.MethodGet, options.OriginURL, nil)
		if err != nil {
			return nil, err
		}

		appInfo, err := token.GetAppInfo(originReq.URL)
		if err != nil {
			return nil, err
		}
		options.AppInfo = appInfo

		wsConn, err = createAccessAuthenticatedStream(options, log)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return &cfwebsocket.GorillaConn{Conn: wsConn}, nil
}

var stripWebsocketHeaders = []string{
	"Upgrade",
	"Connection",
	"Sec-Websocket-Key",
	"Sec-Websocket-Version",
	"Sec-Websocket-Extensions",
}

// the gorilla websocket library sets its own Upgrade, Connection, Sec-WebSocket-Key,
// Sec-WebSocket-Version and Sec-Websocket-Extensions headers.
// https://github.com/gorilla/websocket/blob/master/client.go#L189-L194.
func websocketHeaders(req *http.Request) http.Header {
	wsHeaders := make(http.Header)
	for key, val := range req.Header {
		wsHeaders[key] = val
	}
	// Assume the header keys are in canonical format.
	for _, header := range stripWebsocketHeaders {
		wsHeaders.Del(header)
	}
	wsHeaders.Set("Host", req.Host) // See TUN-1097
	return wsHeaders
}

// clientConnect creates a WebSocket client connection for provided request. Caller is responsible for closing
// the connection. The response body may not contain the entire response and does
// not need to be closed by the application.
func clientConnect(req *http.Request, dialler *websocket.Dialer) (*websocket.Conn, *http.Response, error) {
	req.URL.Scheme = changeRequestScheme(req.URL)
	wsHeaders := websocketHeaders(req)
	if dialler == nil {
		dialler = &websocket.Dialer{
			Proxy: http.ProxyFromEnvironment,
		}
	}
	conn, response, err := dialler.Dial(req.URL.String(), wsHeaders)
	if err != nil {
		return nil, response, err
	}
	return conn, response, nil
}

// changeRequestScheme is needed as the gorilla websocket library requires the ws scheme.
// (even though it changes it back to http/https, but ¯\_(ツ)_/¯.)
func changeRequestScheme(reqURL *url.URL) string {
	switch reqURL.Scheme {
	case "https":
		return "wss"
	case "http":
		return "ws"
	case "":
		return "ws"
	default:
		return reqURL.Scheme
	}
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
	if err := token.RemoveTokenIfExists(options.AppInfo); err != nil {
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

	conn, resp, err := clientConnect(req, nil)

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
