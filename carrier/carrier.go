//Package carrier provides a WebSocket proxy to carry or proxy a connection
//from the local client to the edge. See it as a wrapper around any protocol
//that it packages up in a WebSocket connection to the edge.
package carrier

import (
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/token"
	"github.com/cloudflare/cloudflared/websocket"
	"github.com/sirupsen/logrus"
)

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

// StartClient will copy the data from stdin/stdout over a WebSocket connection
// to the edge (originURL)
func StartClient(logger *logrus.Logger, originURL string, stream io.ReadWriter, headers http.Header) error {
	return serveStream(logger, originURL, stream, headers)
}

// StartServer will setup a listener on a specified address/port and then
// forward connections to the origin by calling `Serve()`.
func StartServer(logger *logrus.Logger, address, originURL string, shutdownC <-chan struct{}, headers http.Header) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.WithError(err).Error("failed to start forwarding server")
		return err
	}
	logger.Info("Started listening on ", address)
	return Serve(logger, listener, originURL, shutdownC, headers)
}

// Serve accepts incoming connections on the specified net.Listener.
// Each connection is handled in a new goroutine: its data is copied over a
// WebSocket connection to the edge (originURL).
// `Serve` always closes `listener`.
func Serve(logger *logrus.Logger, listener net.Listener, originURL string, shutdownC <-chan struct{}, headers http.Header) error {
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
			go serveConnection(logger, conn, originURL, headers)
		}
	}
}

// serveConnection handles connections for the Serve() call
func serveConnection(logger *logrus.Logger, c net.Conn, originURL string, headers http.Header) {
	defer c.Close()
	serveStream(logger, originURL, c, headers)
}

// serveStream will serve the data over the WebSocket stream
func serveStream(logger *logrus.Logger, originURL string, conn io.ReadWriter, headers http.Header) error {
	wsConn, err := createWebsocketStream(originURL, headers)
	if err != nil {
		logger.WithError(err).Errorf("failed to connect to %s\n", originURL)
		return err
	}
	defer wsConn.Close()

	websocket.Stream(wsConn, conn)

	return nil
}

// createWebsocketStream will create a WebSocket connection to stream data over
// It also handles redirects from Access and will present that flow if
// the token is not present on the request
func createWebsocketStream(originURL string, headers http.Header) (*websocket.Conn, error) {
	req, err := http.NewRequest(http.MethodGet, originURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = headers

	wsConn, resp, err := websocket.ClientConnect(req, nil)
	if err != nil && resp != nil && resp.StatusCode > 300 {
		location, err := resp.Location()
		if err != nil {
			return nil, err
		}
		if !strings.Contains(location.String(), "cdn-cgi/access/login") {
			return nil, errors.New("not an Access redirect")
		}
		req, err := buildAccessRequest(originURL)
		if err != nil {
			return nil, err
		}

		wsConn, _, err = websocket.ClientConnect(req, nil)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return &websocket.Conn{Conn: wsConn}, nil
}

// buildAccessRequest builds an HTTP request with the Access token set
func buildAccessRequest(originURL string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, originURL, nil)
	if err != nil {
		return nil, err
	}

	token, err := token.FetchToken(req.URL)
	if err != nil {
		return nil, err
	}

	// We need to create a new request as FetchToken will modify req (boo mutable)
	// as it has to follow redirect on the API and such, so here we init a new one
	originRequest, err := http.NewRequest(http.MethodGet, originURL, nil)
	if err != nil {
		return nil, err
	}
	originRequest.Header.Set("cf-access-token", token)

	return originRequest, nil
}
