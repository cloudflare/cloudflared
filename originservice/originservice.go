// Package client defines and implements interface to proxy to HTTP, websocket and hello world origins
package originservice

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudflare/cloudflared/buffer"
	"github.com/cloudflare/cloudflared/h2mux"
	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/log"
	"github.com/cloudflare/cloudflared/websocket"

	"github.com/pkg/errors"
)

// OriginService is an interface to proxy requests to different type of origins
type OriginService interface {
	Proxy(stream *h2mux.MuxedStream, req *http.Request) (resp *http.Response, err error)
	URL() *url.URL
	Summary() string
	Shutdown()
}

// HTTPService talks to origin using HTTP/HTTPS
type HTTPService struct {
	client          http.RoundTripper
	originURL       *url.URL
	chunkedEncoding bool
	bufferPool      *buffer.Pool
}

func NewHTTPService(transport http.RoundTripper, url *url.URL, chunkedEncoding bool) OriginService {
	return &HTTPService{
		client:          transport,
		originURL:       url,
		chunkedEncoding: chunkedEncoding,
		bufferPool:      buffer.NewPool(512 * 1024),
	}
}

func (hc *HTTPService) Proxy(stream *h2mux.MuxedStream, req *http.Request) (*http.Response, error) {
	// Support for WSGI Servers by switching transfer encoding from chunked to gzip/deflate
	if !hc.chunkedEncoding {
		req.TransferEncoding = []string{"gzip", "deflate"}
		cLength, err := strconv.Atoi(req.Header.Get("Content-Length"))
		if err == nil {
			req.ContentLength = int64(cLength)
		}
	}

	// Request origin to keep connection alive to improve performance
	req.Header.Set("Connection", "keep-alive")

	resp, err := hc.client.RoundTrip(req)
	if err != nil {
		return nil, errors.Wrap(err, "error proxying request to HTTP origin")
	}
	defer resp.Body.Close()

	err = stream.WriteHeaders(h1ResponseToH2Response(resp))
	if err != nil {
		return nil, errors.Wrap(err, "error writing response header to HTTP origin")
	}
	if isEventStream(resp) {
		writeEventStream(stream, resp.Body)
	} else {
		// Use CopyBuffer, because Copy only allocates a 32KiB buffer, and cross-stream
		// compression generates dictionary on first write
		buf := hc.bufferPool.Get()
		defer hc.bufferPool.Put(buf)
		io.CopyBuffer(stream, resp.Body, buf)
	}
	return resp, nil
}

func (hc *HTTPService) URL() *url.URL {
	return hc.originURL
}

func (hc *HTTPService) Summary() string {
	return fmt.Sprintf("HTTP service listening on %s", hc.originURL)
}

func (hc *HTTPService) Shutdown() {}

// WebsocketService talks to origin using WS/WSS
type WebsocketService struct {
	tlsConfig *tls.Config
	originURL *url.URL
	shutdownC chan struct{}
}

func NewWebSocketService(tlsConfig *tls.Config, url *url.URL) (OriginService, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		return nil, errors.Wrap(err, "cannot start Websocket Proxy Server")
	}
	shutdownC := make(chan struct{})
	go func() {
		websocket.StartProxyServer(log.CreateLogger(), listener, url.String(), shutdownC, websocket.DefaultStreamHandler)
	}()
	return &WebsocketService{
		tlsConfig: tlsConfig,
		originURL: url,
		shutdownC: shutdownC,
	}, nil
}

func (wsc *WebsocketService) Proxy(stream *h2mux.MuxedStream, req *http.Request) (*http.Response, error) {
	if !websocket.IsWebSocketUpgrade(req) {
		return nil, fmt.Errorf("request is not a websocket connection")
	}
	conn, response, err := websocket.ClientConnect(req, wsc.tlsConfig)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	err = stream.WriteHeaders(h1ResponseToH2Response(response))
	if err != nil {
		return nil, errors.Wrap(err, "error writing response header to websocket origin")
	}
	// Copy to/from stream to the undelying connection. Use the underlying
	// connection because cloudflared doesn't operate on the message themselves
	websocket.Stream(conn.UnderlyingConn(), stream)
	return response, nil
}

func (wsc *WebsocketService) URL() *url.URL {
	return wsc.originURL
}

func (wsc *WebsocketService) Summary() string {
	return fmt.Sprintf("Websocket listening on %s", wsc.originURL)
}

func (wsc *WebsocketService) Shutdown() {
	close(wsc.shutdownC)
}

// HelloWorldService talks to the hello world example origin
type HelloWorldService struct {
	client     http.RoundTripper
	listener   net.Listener
	originURL  *url.URL
	shutdownC  chan struct{}
	bufferPool *buffer.Pool
}

func NewHelloWorldService(transport http.RoundTripper) (OriginService, error) {
	listener, err := hello.CreateTLSListener("127.0.0.1:")
	if err != nil {
		return nil, errors.Wrap(err, "cannot start Hello World Server")
	}
	shutdownC := make(chan struct{})
	go func() {
		hello.StartHelloWorldServer(log.CreateLogger(), listener, shutdownC)
	}()
	return &HelloWorldService{
		client:   transport,
		listener: listener,
		originURL: &url.URL{
			Scheme: "https",
			Host:   listener.Addr().String(),
		},
		shutdownC:  shutdownC,
		bufferPool: buffer.NewPool(512 * 1024),
	}, nil
}

func (hwc *HelloWorldService) Proxy(stream *h2mux.MuxedStream, req *http.Request) (*http.Response, error) {
	// Request origin to keep connection alive to improve performance
	req.Header.Set("Connection", "keep-alive")
	resp, err := hwc.client.RoundTrip(req)
	if err != nil {
		return nil, errors.Wrap(err, "error proxying request to Hello World origin")
	}
	defer resp.Body.Close()

	err = stream.WriteHeaders(h1ResponseToH2Response(resp))
	if err != nil {
		return nil, errors.Wrap(err, "error writing response header to Hello World origin")
	}

	// Use CopyBuffer, because Copy only allocates a 32KiB buffer, and cross-stream
	// compression generates dictionary on first write
	buf := hwc.bufferPool.Get()
	defer hwc.bufferPool.Put(buf)
	io.CopyBuffer(stream, resp.Body, buf)

	return resp, nil
}

func (hwc *HelloWorldService) URL() *url.URL {
	return hwc.originURL
}

func (hwc *HelloWorldService) Summary() string {
	return fmt.Sprintf("Hello World service listening on %s", hwc.originURL)
}

func (hwc *HelloWorldService) Shutdown() {
	hwc.listener.Close()
}

func isEventStream(resp *http.Response) bool {
	// Check if content-type is text/event-stream. We need to check if the header value starts with text/event-stream
	// because text/event-stream; charset=UTF-8 is also valid
	// Ref: https://tools.ietf.org/html/rfc7231#section-3.1.1.1
	for _, contentType := range resp.Header["content-type"] {
		if strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") {
			return true
		}
	}
	return false
}

func writeEventStream(stream *h2mux.MuxedStream, respBody io.ReadCloser) {
	reader := bufio.NewReader(respBody)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			break
		}
		stream.Write(line)
	}
}

func h1ResponseToH2Response(h1 *http.Response) (h2 []h2mux.Header) {
	h2 = []h2mux.Header{{Name: ":status", Value: fmt.Sprintf("%d", h1.StatusCode)}}
	for headerName, headerValues := range h1.Header {
		for _, headerValue := range headerValues {
			h2 = append(h2, h2mux.Header{Name: strings.ToLower(headerName), Value: headerValue})
		}
	}
	return
}
