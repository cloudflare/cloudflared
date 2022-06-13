package ingress

import (
	"context"
	"fmt"
	"net/http"
)

// HTTPOriginProxy can be implemented by origin services that want to proxy http requests.
type HTTPOriginProxy interface {
	// RoundTripper is how cloudflared proxies eyeball requests to the actual origin services
	http.RoundTripper
}

// StreamBasedOriginProxy can be implemented by origin services that want to proxy ws/TCP.
type StreamBasedOriginProxy interface {
	EstablishConnection(ctx context.Context, dest string) (OriginConnection, error)
}

func (o *unixSocketPath) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = o.scheme
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
		// Pass the original Host header as X-Forwarded-Host.
		req.Header.Set("X-Forwarded-Host", req.Host)
		req.Host = o.hostHeader
	}
	return o.transport.RoundTrip(req)
}

func (o *statusCode) RoundTrip(_ *http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode: o.code,
		Status:     fmt.Sprintf("%d %s", o.code, http.StatusText(o.code)),
		Body:       new(NopReadCloser),
	}

	return resp, nil
}

func (o *rawTCPService) EstablishConnection(ctx context.Context, dest string) (OriginConnection, error) {
	conn, err := o.dialer.DialContext(ctx, "tcp", dest)
	if err != nil {
		return nil, err
	}

	originConn := &tcpConnection{
		conn: conn,
	}
	return originConn, nil
}

func (o *tcpOverWSService) EstablishConnection(ctx context.Context, dest string) (OriginConnection, error) {
	var err error
	if !o.isBastion {
		dest = o.dest
	}

	conn, err := o.dialer.DialContext(ctx, "tcp", dest)
	if err != nil {
		return nil, err
	}
	originConn := &tcpOverWSConnection{
		conn:          conn,
		streamHandler: o.streamHandler,
	}
	return originConn, nil

}

func (o *socksProxyOverWSService) EstablishConnection(_ctx context.Context, _dest string) (OriginConnection, error) {
	return o.conn, nil
}
