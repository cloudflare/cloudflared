//Package carrier provides a WebSocket proxy to carry or proxy a connection
//from the local client to the edge. See it as a wrapper around any protocol
//that it packages up in a WebSocket connection to the edge.
package carrier

import (
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/token"
	cloudflaredWebsocket "github.com/cloudflare/cloudflared/websocket"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type StartOptions struct {
	OriginURL string
	Headers   http.Header
}

// StdinoutStream is empty struct for wrapping stdin/stdout
// into a single ReadWriter
type StdinoutStream struct {
}

// Read will read from Stdin
func (c *StdinoutStream) Read(p []byte) (int, error) {
	return os.Stdin.Read(p)

}

// Write will write to Stdout
func (c *StdinoutStream) Write(p []byte) (int, error) {
	return os.Stdout.Write(p)
}

// Helper to allow defering the response close with a check that the resp is not nil
func closeRespBody(resp *http.Response) {
	if resp != nil {
		resp.Body.Close()
	}
}

// StartClient will copy the data from stdin/stdout over a WebSocket connection
// to the edge (originURL)
func StartClient(logger *logrus.Logger, stream io.ReadWriter, options *StartOptions) error {
	return serveStream(logger, stream, options)
}

// StartServer will setup a listener on a specified address/port and then
// forward connections to the origin by calling `Serve()`.
func StartServer(logger *logrus.Logger, address string, shutdownC <-chan struct{}, options *StartOptions) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.WithError(err).Error("failed to start forwarding server")
		return err
	}
	logger.Info("Started listening on ", address)
	return Serve(logger, listener, shutdownC, options)
}

// Serve accepts incoming connections on the specified net.Listener.
// Each connection is handled in a new goroutine: its data is copied over a
// WebSocket connection to the edge (originURL).
// `Serve` always closes `listener`.
func Serve(logger *logrus.Logger, listener net.Listener, shutdownC <-chan struct{}, options *StartOptions) error {
	defer listener.Close()
	for {
		select {
		case <-shutdownC:
			return nil
		default:
			conn, err := listener.Accept()
			if err != nil {
				return err
			}
			go serveConnection(logger, conn, options)
		}
	}
}

// serveConnection handles connections for the Serve() call
func serveConnection(logger *logrus.Logger, c net.Conn, options *StartOptions) {
	defer c.Close()
	serveStream(logger, c, options)
}

// serveStream will serve the data over the WebSocket stream
func serveStream(logger *logrus.Logger, conn io.ReadWriter, options *StartOptions) error {
	wsConn, err := createWebsocketStream(options)
	if err != nil {
		logger.WithError(err).Errorf("failed to connect to %s\n", options.OriginURL)
		return err
	}
	defer wsConn.Close()

	cloudflaredWebsocket.Stream(wsConn, conn)

	return nil
}

// createWebsocketStream will create a WebSocket connection to stream data over
// It also handles redirects from Access and will present that flow if
// the token is not present on the request
func createWebsocketStream(options *StartOptions) (*cloudflaredWebsocket.Conn, error) {
	req, err := http.NewRequest(http.MethodGet, options.OriginURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = options.Headers

	wsConn, resp, err := cloudflaredWebsocket.ClientConnect(req, nil)
	defer closeRespBody(resp)
	if err != nil && isAccessResponse(resp) {
		wsConn, err = createAccessAuthenticatedStream(options)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return &cloudflaredWebsocket.Conn{Conn: wsConn}, nil
}

// isAccessResponse checks the http Response to see if the url location
// contains the Access structure.
func isAccessResponse(resp *http.Response) bool {
	if resp == nil || resp.StatusCode <= 300 {
		return false
	}

	location, err := resp.Location()
	if err != nil || location == nil {
		return false
	}
	if strings.HasPrefix(location.Path, "/cdn-cgi/access/login") {
		return true
	}

	return false
}

// createAccessAuthenticatedStream will try load a token from storage and make
// a connection with the token set on the request. If it still get redirect,
// this probably means the token in storage is invalid (expired/revoked). If that
// happens it deletes the token and runs the connection again, so the user can
// login again and generate a new one.
func createAccessAuthenticatedStream(options *StartOptions) (*websocket.Conn, error) {
	wsConn, resp, err := createAccessWebSocketStream(options)
	defer closeRespBody(resp)
	if err == nil {
		return wsConn, nil
	}

	if !isAccessResponse(resp) {
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
	wsConn, resp, err = createAccessWebSocketStream(options)
	defer closeRespBody(resp)
	if err != nil {
		return nil, err
	}

	return wsConn, nil
}

// createAccessWebSocketStream builds an Access request and makes a connection
func createAccessWebSocketStream(options *StartOptions) (*websocket.Conn, *http.Response, error) {
	req, err := buildAccessRequest(options)
	if err != nil {
		return nil, nil, err
	}
	return cloudflaredWebsocket.ClientConnect(req, nil)
}

// buildAccessRequest builds an HTTP request with the Access token set
func buildAccessRequest(options *StartOptions) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, options.OriginURL, nil)
	if err != nil {
		return nil, err
	}

	token, err := token.FetchToken(req.URL)
	if err != nil {
		return nil, err
	}

	// We need to create a new request as FetchToken will modify req (boo mutable)
	// as it has to follow redirect on the API and such, so here we init a new one
	originRequest, err := http.NewRequest(http.MethodGet, options.OriginURL, nil)
	if err != nil {
		return nil, err
	}
	originRequest.Header.Set("cf-access-token", token)

	for k, v := range options.Headers {
		if len(v) >= 1 {
			originRequest.Header.Set(k, v[0])
		}
	}

	return originRequest, nil
}
