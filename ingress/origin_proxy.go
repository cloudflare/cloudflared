package ingress

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/pkg/errors"

	"github.com/cloudflare/cloudflared/carrier"
	"github.com/cloudflare/cloudflared/websocket"
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
	EstablishConnection(r *http.Request) (OriginConnection, *http.Response, error)
}

func (o *unixSocketPath) RoundTrip(req *http.Request) (*http.Response, error) {
	return o.transport.RoundTrip(req)
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
	req = req.Clone(req.Context())

	req.URL.Host = o.url.Host
	req.URL.Scheme = o.url.Scheme
	// allow ws(s) scheme for websocket-only origins, normal http(s) requests will fail
	switch req.URL.Scheme {
	case "ws":
		req.URL.Scheme = "http"
	case "wss":
		req.URL.Scheme = "https"
	}

	if o.hostHeader != "" {
		// For incoming requests, the Host header is promoted to the Request.Host field and removed from the Header map.
		req.Host = o.hostHeader
	}

	return o.newWebsocketProxyConnection(req)
}

func (o *httpService) newWebsocketProxyConnection(req *http.Request) (OriginConnection, *http.Response, error) {
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")

	req.ContentLength = 0
	req.Body = nil

	resp, err := o.transport.RoundTrip(req)
	if err != nil {
		return nil, nil, err
	}

	toClose := resp.Body
	defer func() {
		if toClose != nil {
			_ = toClose.Close()
		}
	}()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, nil, fmt.Errorf("unexpected origin response: %s", resp.Status)
	}
	if strings.ToLower(resp.Header.Get("Upgrade")) != "websocket" {
		return nil, nil, fmt.Errorf("unexpected upgrade: %q", resp.Header.Get("Upgrade"))
	}

	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		return nil, nil, errUnsupportedConnectionType
	}
	conn := wsProxyConnection{
		rwc: rwc,
	}
	// clear to prevent defer from closing
	toClose = nil

	return &conn, resp, nil
}

func (o *statusCode) RoundTrip(_ *http.Request) (*http.Response, error) {
	return o.resp, nil
}

func (o *rawTCPService) EstablishConnection(r *http.Request) (OriginConnection, *http.Response, error) {
	dest, err := getRequestHost(r)
	if err != nil {
		return nil, nil, err
	}
	conn, err := net.Dial("tcp", dest)
	if err != nil {
		return nil, nil, err
	}

	originConn := &tcpConnection{
		conn: conn,
	}
	resp := &http.Response{
		Status:        switchingProtocolText,
		StatusCode:    http.StatusSwitchingProtocols,
		ContentLength: -1,
	}
	return originConn, resp, nil
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

func (o *tcpOverWSService) EstablishConnection(r *http.Request) (OriginConnection, *http.Response, error) {
	var err error
	dest := o.dest
	if o.isBastion {
		dest, err = carrier.ResolveBastionDest(r)
		if err != nil {
			return nil, nil, err
		}
	}

	conn, err := net.Dial("tcp", dest)
	if err != nil {
		return nil, nil, err
	}
	originConn := &tcpOverWSConnection{
		conn:          conn,
		streamHandler: o.streamHandler,
	}
	resp := &http.Response{
		Status:        switchingProtocolText,
		StatusCode:    http.StatusSwitchingProtocols,
		Header:        websocket.NewResponseHeader(r),
		ContentLength: -1,
	}
	return originConn, resp, nil

}

func (o *socksProxyOverWSService) EstablishConnection(r *http.Request) (OriginConnection, *http.Response, error) {
	originConn := o.conn
	resp := &http.Response{
		Status:        switchingProtocolText,
		StatusCode:    http.StatusSwitchingProtocols,
		Header:        websocket.NewResponseHeader(r),
		ContentLength: -1,
	}
	return originConn, resp, nil
}
