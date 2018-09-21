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

	"github.com/cloudflare/cloudflared/cmd/cloudflared/access"
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
func StartClient(logger *logrus.Logger, originURL string, stream io.ReadWriter) error {
	return serveStream(logger, originURL, stream)
}

// StartServer will setup a server on a specified port and copy data over a WebSocket connection
// to the edge (originURL)
func StartServer(logger *logrus.Logger, address, originURL string, shutdownC <-chan struct{}) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		logger.WithError(err).Error("failed to start forwarding server")
		return err
	}
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
			go serveConnection(logger, conn, originURL)
		}
	}
}

// serveConnection handles connections for the StartServer call
func serveConnection(logger *logrus.Logger, c net.Conn, originURL string) {
	defer c.Close()
	serveStream(logger, originURL, c)
}

// serveStream will serve the data over the WebSocket stream
func serveStream(logger *logrus.Logger, originURL string, conn io.ReadWriter) error {
	wsConn, err := createWebsocketStream(originURL)
	if err != nil {
		logger.WithError(err).Error("failed to create websocket stream")
		return err
	}
	defer wsConn.Close()

	websocket.Stream(wsConn, conn)

	return nil
}

// createWebsocketStream will create a WebSocket connection to stream data over
// It also handles redirects from Access and will present that flow if
// the token is not present on the request
func createWebsocketStream(originURL string) (*websocket.Conn, error) {
	req, err := http.NewRequest(http.MethodGet, originURL, nil)
	if err != nil {
		return nil, err
	}
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

	token, err := access.FetchToken(req.URL)
	if err != nil {
		return nil, err
	}
	req.Header.Set("cf-access-token", token)

	return req, nil
}
