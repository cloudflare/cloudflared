package ingress

import (
	"fmt"
	"net"
	"net/http"

	"github.com/pkg/errors"
)

var (
	switchingProtocolText        = fmt.Sprintf("%d %s", http.StatusSwitchingProtocols, http.StatusText(http.StatusSwitchingProtocols))
	errUnsupportedConnectionType = errors.New("internal error: unsupported connection type")
)

// HTTPOriginProxy can be implemented by origin services that want to proxy http requests.
type HTTPOriginProxy interface {
	// RoundTrip is how cloudflared proxies eyeball requests to the actual origin services
	http.RoundTripper
}

// StreamBasedOriginProxy can be implemented by origin services that want to proxy ws/TCP.
type StreamBasedOriginProxy interface {
	EstablishConnection(dest string) (OriginConnection, error)
}

func (o *unixSocketPath) RoundTrip(req *http.Request) (*http.Response, error) {
	return o.transport.RoundTrip(req)
}

func (o *httpService) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the request URL so that it goes to the origin service.
	req.URL.Host = o.url.Host
	switch o.url.Scheme {
	case "ws":
		req.URL.Scheme = "http"
	case "wss":
		req.URL.Scheme = "https"
	default:
		req.URL.Scheme = o.url.Scheme
	}

	if o.hostHeader != "" {
		// For incoming requests, the Host header is promoted to the Request.Host field and removed from the Header map.
		req.Host = o.hostHeader
	}
	return o.transport.RoundTrip(req)
}

func (o *statusCode) RoundTrip(_ *http.Request) (*http.Response, error) {
	return o.resp, nil
}

func (o *rawTCPService) EstablishConnection(dest string) (OriginConnection, error) {
	conn, err := net.Dial("tcp", dest)
	if err != nil {
		return nil, err
	}

	originConn := &tcpConnection{
		conn: conn,
	}
	return originConn, nil
}

func (o *tcpOverWSService) EstablishConnection(dest string) (OriginConnection, error) {
	var err error
	if !o.isBastion {
		dest = o.dest
	}

	conn, err := net.Dial("tcp", dest)
	if err != nil {
		return nil, err
	}
	originConn := &tcpOverWSConnection{
		conn:          conn,
		streamHandler: o.streamHandler,
	}
	return originConn, nil

}

func (o *socksProxyOverWSService) EstablishConnection(dest string) (OriginConnection, error) {
	return o.conn, nil
}
