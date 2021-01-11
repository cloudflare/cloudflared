package websocket

import (
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

var stripWebsocketHeaders = []string{
	"Upgrade",
	"Connection",
	"Sec-Websocket-Key",
	"Sec-Websocket-Version",
	"Sec-Websocket-Extensions",
}

// IsWebSocketUpgrade checks to see if the request is a WebSocket connection.
func IsWebSocketUpgrade(req *http.Request) bool {
	return websocket.IsWebSocketUpgrade(req)
}

// ClientConnect creates a WebSocket client connection for provided request. Caller is responsible for closing
// the connection. The response body may not contain the entire response and does
// not need to be closed by the application.
func ClientConnect(req *http.Request, dialler *websocket.Dialer) (*websocket.Conn, *http.Response, error) {
	req.URL.Scheme = ChangeRequestScheme(req.URL)
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
	response.Header.Set("Sec-WebSocket-Accept", generateAcceptKey(req))
	return conn, response, nil
}

// StartProxyServer will start a websocket server that will decode
// the websocket data and write the resulting data to the provided
func StartProxyServer(
	log *zerolog.Logger,
	listener net.Listener,
	staticHost string,
	shutdownC <-chan struct{},
	streamHandler func(originConn io.ReadWriter, remoteConn net.Conn),
) error {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	h := handler{
		upgrader:      upgrader,
		log:           log,
		staticHost:    staticHost,
		streamHandler: streamHandler,
	}

	httpServer := &http.Server{Addr: listener.Addr().String(), Handler: &h}
	go func() {
		<-shutdownC
		_ = httpServer.Close()
	}()

	return httpServer.Serve(listener)
}

// HTTP handler for the websocket proxy.
type handler struct {
	log           *zerolog.Logger
	staticHost    string
	upgrader      websocket.Upgrader
	streamHandler func(originConn io.ReadWriter, remoteConn net.Conn)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// If remote is an empty string, get the destination from the client.
	finalDestination := h.staticHost
	if finalDestination == "" {
		if jumpDestination := r.Header.Get(h2mux.CFJumpDestinationHeader); jumpDestination == "" {
			h.log.Error().Msg("Did not receive final destination from client. The --destination flag is likely not set")
			return
		} else {
			finalDestination = jumpDestination
		}
	}

	stream, err := net.Dial("tcp", finalDestination)
	if err != nil {
		h.log.Err(err).Msg("Cannot connect to remote")
		return
	}
	defer stream.Close()

	if !websocket.IsWebSocketUpgrade(r) {
		_, _ = w.Write(nonWebSocketRequestPage())
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Err(err).Msg("failed to upgrade")
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { _ = conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	gorillaConn := &GorillaConn{conn, h.log}
	go gorillaConn.pinger(r.Context())
	defer conn.Close()

	h.streamHandler(gorillaConn, stream)
}

// NewResponseHeader returns headers needed to return to origin for completing handshake
func NewResponseHeader(req *http.Request) http.Header {
	header := http.Header{}
	header.Add("Connection", "Upgrade")
	header.Add("Sec-Websocket-Accept", generateAcceptKey(req))
	header.Add("Upgrade", "websocket")
	return header
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

// sha1Base64 sha1 and then base64 encodes str.
func sha1Base64(str string) string {
	hasher := sha1.New()
	_, _ = io.WriteString(hasher, str)
	hash := hasher.Sum(nil)
	return base64.StdEncoding.EncodeToString(hash)
}

// generateAcceptKey returns the string needed for the Sec-WebSocket-Accept header.
// https://tools.ietf.org/html/rfc6455#section-1.3 describes this process in more detail.
func generateAcceptKey(req *http.Request) string {
	return sha1Base64(req.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
}

// ChangeRequestScheme is needed as the gorilla websocket library requires the ws scheme.
// (even though it changes it back to http/https, but ¯\_(ツ)_/¯.)
func ChangeRequestScheme(reqURL *url.URL) string {
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
