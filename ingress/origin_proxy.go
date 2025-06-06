package ingress

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"

	"github.com/rs/zerolog"
)

// HTTPOriginProxy can be implemented by origin services that want to proxy http requests.
type HTTPOriginProxy interface {
	// RoundTripper is how cloudflared proxies eyeball requests to the actual origin services
	http.RoundTripper
}

// StreamBasedOriginProxy can be implemented by origin services that want to proxy ws/TCP.
type StreamBasedOriginProxy interface {
	EstablishConnection(ctx context.Context, dest string, log *zerolog.Logger) (OriginConnection, error)
}

// HTTPLocalProxy can be implemented by cloudflared services that want to handle incoming http requests.
type HTTPLocalProxy interface {
	// Handler is how cloudflared proxies eyeball requests to the local cloudflared services
	http.Handler
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

	if o.matchSNIToHost {
		o.SetOriginServerName(req)
	}

	return o.transport.RoundTrip(req)
}

func (o *httpService) SetOriginServerName(req *http.Request) {
	o.transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := o.transport.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return tls.Client(conn, &tls.Config{
			RootCAs:            o.transport.TLSClientConfig.RootCAs,
			InsecureSkipVerify: o.transport.TLSClientConfig.InsecureSkipVerify, // nolint: gosec
			ServerName:         req.Host,
		}), nil
	}
}

func (o *statusCode) RoundTrip(_ *http.Request) (*http.Response, error) {
	if o.defaultResp {
		o.log.Warn().Msg(ErrNoIngressRulesCLI.Error())
	}
	resp := &http.Response{
		StatusCode: o.code,
		Status:     fmt.Sprintf("%d %s", o.code, http.StatusText(o.code)),
		Body:       new(NopReadCloser),
	}

	return resp, nil
}

func (o *rawTCPService) EstablishConnection(ctx context.Context, dest string, logger *zerolog.Logger) (OriginConnection, error) {
	conn, err := o.dialer.DialContext(ctx, "tcp", dest)
	if err != nil {
		return nil, err
	}

	originConn := &tcpConnection{
		Conn:         conn,
		writeTimeout: o.writeTimeout,
		logger:       logger,
	}
	return originConn, nil
}

func (o *tcpOverWSService) EstablishConnection(ctx context.Context, dest string, _ *zerolog.Logger) (OriginConnection, error) {
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

func (o *socksProxyOverWSService) EstablishConnection(_ context.Context, _ string, _ *zerolog.Logger) (OriginConnection, error) {
	return o.conn, nil
}
