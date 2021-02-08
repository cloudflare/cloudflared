package ingress

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/websocket"
	"github.com/pkg/errors"
)

// HTTPOriginProxy can be implemented by origin services that want to proxy http requests.
type HTTPOriginProxy interface {
	// RoundTrip is how cloudflared proxies eyeball requests to the actual origin services
	http.RoundTripper
}

// StreamBasedOriginProxy can be implemented by origin services that want to proxy at the L4 level.
type StreamBasedOriginProxy interface {
	EstablishConnection(r *http.Request) (OriginConnection, *http.Response, error)
}

func (o *unixSocketPath) RoundTrip(req *http.Request) (*http.Response, error) {
	return o.transport.RoundTrip(req)
}

// TODO: TUN-3636: establish connection to origins over UDS
func (*unixSocketPath) EstablishConnection(r *http.Request) (OriginConnection, *http.Response, error) {
	return nil, nil, fmt.Errorf("Unix socket service currently doesn't support proxying connections")
}

func (o *httpService) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the request URL so that it goes to the origin service.
	req.URL.Host = o.url.Host
	req.URL.Scheme = o.url.Scheme
	if o.hostHeader != "" {
		// For incoming requests, the Host header is promoted to the Request.Host field and removed from the Header map.
		req.Host = o.hostHeader
	}
	return o.transport.RoundTrip(req)
}

func (o *httpService) EstablishConnection(req *http.Request) (OriginConnection, *http.Response, error) {
	req.URL.Host = o.url.Host
	req.URL.Scheme = websocket.ChangeRequestScheme(o.url)
	if o.hostHeader != "" {
		// For incoming requests, the Host header is promoted to the Request.Host field and removed from the Header map.
		req.Host = o.hostHeader
	}
	return newWSConnection(o.transport, req)
}

func (o *helloWorld) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the request URL so that it goes to the Hello World server.
	req.URL.Host = o.server.Addr().String()
	req.URL.Scheme = "https"
	return o.transport.RoundTrip(req)
}

func (o *helloWorld) EstablishConnection(req *http.Request) (OriginConnection, *http.Response, error) {
	req.URL.Host = o.server.Addr().String()
	req.URL.Scheme = "wss"
	return newWSConnection(o.transport, req)
}

func (o *statusCode) RoundTrip(_ *http.Request) (*http.Response, error) {
	return o.resp, nil
}

func (o *bridgeService) EstablishConnection(r *http.Request) (OriginConnection, *http.Response, error) {
	dest, err := o.destination(r)
	if err != nil {
		return nil, nil, err
	}
	conn, err := o.client.connect(r, dest)
	return conn, nil, err
}

// getRequestHost returns the host of the http.Request.
func getRequestHost(r *http.Request) (string, error) {
	if r.Host != "" {
		return r.Host, nil
	}
	if r.URL != nil {
		return r.URL.Host, nil
	}
	return "", errors.New("host not found")
}

func (o *bridgeService) destination(r *http.Request) (string, error) {
	if connection.IsTCPStream(r) {
		return getRequestHost(r)
	}
	jumpDestination := r.Header.Get(h2mux.CFJumpDestinationHeader)
	if jumpDestination == "" {
		return "", fmt.Errorf("Did not receive final destination from client. The --destination flag is likely not set on the client side")
	}
	// Strip scheme and path set by client. Without a scheme
	// Parsing a hostname and path without scheme might not return an error due to parsing ambiguities
	if jumpURL, err := url.Parse(jumpDestination); err == nil && jumpURL.Host != "" {
		return removePath(jumpURL.Host), nil
	}
	return removePath(jumpDestination), nil
}

func removePath(dest string) string {
	return strings.SplitN(dest, "/", 2)[0]
}

func (o *singleTCPService) EstablishConnection(r *http.Request) (OriginConnection, *http.Response, error) {
	conn, err := o.client.connect(r, o.dest)
	return conn, nil, err

}

type tcpClient struct {
	streamHandler streamHandlerFunc
}

func (c *tcpClient) connect(r *http.Request, addr string) (OriginConnection, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &tcpConnection{
		conn:          conn,
		streamHandler: c.streamHandler,
	}, nil
}
