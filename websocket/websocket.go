package websocket

import (
	"bufio"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/sshserver"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

var stripWebsocketHeaders = []string{
	"Upgrade",
	"Connection",
	"Sec-Websocket-Key",
	"Sec-Websocket-Version",
	"Sec-Websocket-Extensions",
}

// Conn is a wrapper around the standard gorilla websocket
// but implements a ReadWriter
type Conn struct {
	*websocket.Conn
}

// Read will read messages from the websocket connection
func (c *Conn) Read(p []byte) (int, error) {
	_, message, err := c.Conn.ReadMessage()
	if err != nil {
		return 0, err
	}

	return copy(p, message), nil

}

// Write will write messages to the websocket connection
func (c *Conn) Write(p []byte) (int, error) {
	if err := c.Conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}

	return len(p), nil
}

// IsWebSocketUpgrade checks to see if the request is a WebSocket connection.
func IsWebSocketUpgrade(req *http.Request) bool {
	return websocket.IsWebSocketUpgrade(req)
}

// ClientConnect creates a WebSocket client connection for provided request. Caller is responsible for closing
// the connection. The response body may not contain the entire response and does
// not need to be closed by the application.
func ClientConnect(req *http.Request, tlsClientConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
	req.URL.Scheme = changeRequestScheme(req)
	wsHeaders := websocketHeaders(req)

	d := &websocket.Dialer{TLSClientConfig: tlsClientConfig}
	conn, response, err := d.Dial(req.URL.String(), wsHeaders)
	if err != nil {
		return nil, response, err
	}
	response.Header.Set("Sec-WebSocket-Accept", generateAcceptKey(req))
	return conn, response, err
}

// HijackConnection takes over an HTTP connection. Caller is responsible for closing connection.
func HijackConnection(w http.ResponseWriter) (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack error")
	}

	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, nil, err
	}
	return conn, brw, nil
}

// Stream copies copy data to & from provided io.ReadWriters.
func Stream(conn, backendConn io.ReadWriter) {
	proxyDone := make(chan struct{}, 2)

	go func() {
		io.Copy(conn, backendConn)
		proxyDone <- struct{}{}
	}()

	go func() {
		io.Copy(backendConn, conn)
		proxyDone <- struct{}{}
	}()

	// If one side is done, we are done.
	<-proxyDone
}

// DefaultStreamHandler is provided to the the standard websocket to origin stream
// This exist to allow SOCKS to deframe data before it gets to the origin
func DefaultStreamHandler(wsConn *Conn, remoteConn net.Conn, _ http.Header) {
	Stream(wsConn, remoteConn)
}

// StartProxyServer will start a websocket server that will decode
// the websocket data and write the resulting data to the provided
func StartProxyServer(logger *logrus.Logger, listener net.Listener, staticHost string, shutdownC <-chan struct{}, streamHandler func(wsConn *Conn, remoteConn net.Conn, requestHeaders http.Header)) error {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	httpServer := &http.Server{Addr: listener.Addr().String(), Handler: nil}
	go func() {
		<-shutdownC
		httpServer.Close()
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If remote is an empty string, get the destination from the client.
		finalDestination := staticHost
		if finalDestination == "" {
			if jumpDestination := r.Header.Get(h2mux.CFJumpDestinationHeader); jumpDestination == "" {
				logger.Error("Did not receive final destination from client. The --destination flag is likely not set")
				return
			} else {
				finalDestination = jumpDestination
			}
		}

		stream, err := net.Dial("tcp", finalDestination)
		if err != nil {
			logger.WithError(err).Error("Cannot connect to remote.")
			return
		}
		defer stream.Close()

		if !websocket.IsWebSocketUpgrade(r) {
			w.Write(nonWebSocketRequestPage())
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.WithError(err).Error("failed to upgrade")
			return
		}
		conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
		done := make(chan struct{})
		go pinger(logger, conn, done)
		defer func() {
			done <- struct{}{}
			conn.Close()
		}()

		streamHandler(&Conn{conn}, stream, r.Header)
	})

	return httpServer.Serve(listener)
}

// SendSSHPreamble sends the final SSH destination address to the cloudflared SSH proxy
// The destination is preceded by its length
// Not part of sshserver module to fix compilation for incompatible operating systems
func SendSSHPreamble(stream net.Conn, destination, token string) error {
	preamble := sshserver.SSHPreamble{Destination: destination, JWT: token}
	payload, err := json.Marshal(preamble)
	if err != nil {
		return err
	}

	if uint16(len(payload)) > ^uint16(0) {
		return errors.New("ssh preamble payload too large")
	}

	sizeBytes := make([]byte, sshserver.SSHPreambleLength)
	binary.BigEndian.PutUint16(sizeBytes, uint16(len(payload)))
	if _, err := stream.Write(sizeBytes); err != nil {
		return err
	}

	if _, err := stream.Write(payload); err != nil {
		return err
	}
	return nil
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
	io.WriteString(hasher, str)
	hash := hasher.Sum(nil)
	return base64.StdEncoding.EncodeToString(hash)
}

// generateAcceptKey returns the string needed for the Sec-WebSocket-Accept header.
// https://tools.ietf.org/html/rfc6455#section-1.3 describes this process in more detail.
func generateAcceptKey(req *http.Request) string {
	return sha1Base64(req.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")
}

// changeRequestScheme is needed as the gorilla websocket library requires the ws scheme.
// (even though it changes it back to http/https, but ¯\_(ツ)_/¯.)
func changeRequestScheme(req *http.Request) string {
	switch req.URL.Scheme {
	case "https":
		return "wss"
	case "http":
		return "ws"
	default:
		return req.URL.Scheme
	}
}

// pinger simulates the websocket connection to keep it alive
func pinger(logger *logrus.Logger, ws *websocket.Conn, done chan struct{}) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := ws.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(writeWait)); err != nil {
				logger.WithError(err).Debug("failed to send ping message")
			}
		case <-done:
			return
		}
	}
}
