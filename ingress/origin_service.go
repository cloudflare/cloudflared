package ingress

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/socks"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/websocket"
	gws "github.com/gorilla/websocket"
	"github.com/pkg/errors"
)

// OriginService is something a tunnel can proxy traffic to.
type OriginService interface {
	// RoundTrip is how cloudflared proxies eyeball requests to the actual origin services
	http.RoundTripper
	String() string
	// Start the origin service if it's managed by cloudflared, e.g. proxy servers or Hello World.
	// If it's not managed by cloudflared, this is a no-op because the user is responsible for
	// starting the origin service.
	start(wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error
}

// unixSocketPath is an OriginService representing a unix socket (which accepts HTTP)
type unixSocketPath struct {
	path      string
	transport *http.Transport
}

func (o *unixSocketPath) String() string {
	return "unix socket: " + o.path
}

func (o *unixSocketPath) start(wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	transport, err := newHTTPTransport(o, cfg)
	if err != nil {
		return err
	}
	o.transport = transport
	return nil
}

func (o *unixSocketPath) RoundTrip(req *http.Request) (*http.Response, error) {
	return o.transport.RoundTrip(req)
}

func (o *unixSocketPath) Dial(reqURL *url.URL, headers http.Header) (*gws.Conn, *http.Response, error) {
	d := &gws.Dialer{
		NetDial:         o.transport.Dial,
		NetDialContext:  o.transport.DialContext,
		TLSClientConfig: o.transport.TLSClientConfig,
	}
	reqURL.Scheme = websocket.ChangeRequestScheme(reqURL)
	return d.Dial(reqURL.String(), headers)
}

// localService is an OriginService listening on a TCP/IP address the user's origin can route to.
type localService struct {
	// The URL for the user's origin service
	RootURL *url.URL
	// The URL that cloudflared should send requests to.
	// If this origin requires starting a proxy, this is the proxy's address,
	// and that proxy points to RootURL. Otherwise, this is equal to RootURL.
	URL       *url.URL
	transport *http.Transport
}

func (o *localService) Dial(reqURL *url.URL, headers http.Header) (*gws.Conn, *http.Response, error) {
	d := &gws.Dialer{TLSClientConfig: o.transport.TLSClientConfig}
	// Rewrite the request URL so that it goes to the origin service.
	reqURL.Host = o.URL.Host
	reqURL.Scheme = websocket.ChangeRequestScheme(o.URL)
	return d.Dial(reqURL.String(), headers)
}

func (o *localService) address() string {
	return o.URL.String()
}

func (o *localService) start(wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	transport, err := newHTTPTransport(o, cfg)
	if err != nil {
		return err
	}
	o.transport = transport

	// Start a proxy if one is needed
	staticHost := o.staticHost()
	if originRequiresProxy(staticHost, cfg) {
		if err := o.startProxy(staticHost, wg, log, shutdownC, errC, cfg); err != nil {
			return err
		}
	}

	return nil
}

func (o *localService) startProxy(staticHost string, wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {

	// Start a listener for the proxy
	proxyAddress := net.JoinHostPort(cfg.ProxyAddress, strconv.Itoa(int(cfg.ProxyPort)))
	listener, err := net.Listen("tcp", proxyAddress)
	if err != nil {
		log.Errorf("Cannot start Websocket Proxy Server: %s", err)
		return errors.Wrap(err, "Cannot start Websocket Proxy Server")
	}

	// Start the proxy itself
	wg.Add(1)
	go func() {
		defer wg.Done()
		streamHandler := websocket.DefaultStreamHandler
		// This origin's config specifies what type of proxy to start.
		switch cfg.ProxyType {
		case socksProxy:
			log.Info("SOCKS5 server started")
			streamHandler = func(wsConn *websocket.Conn, remoteConn net.Conn, _ http.Header) {
				dialer := socks.NewConnDialer(remoteConn)
				requestHandler := socks.NewRequestHandler(dialer)
				socksServer := socks.NewConnectionHandler(requestHandler)

				socksServer.Serve(wsConn)
			}
		case "":
			log.Debug("Not starting any websocket proxy")
		default:
			log.Errorf("%s isn't a valid proxy (valid options are {%s})", cfg.ProxyType, socksProxy)
		}

		errC <- websocket.StartProxyServer(log, listener, staticHost, shutdownC, streamHandler)
	}()

	// Modify this origin, so that it no longer points at the origin service directly.
	// Instead, it points at the proxy to the origin service.
	newURL, err := url.Parse("http://" + listener.Addr().String())
	if err != nil {
		return err
	}
	o.URL = newURL
	return nil
}

func (o *localService) String() string {
	return o.address()
}

func (o *localService) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the request URL so that it goes to the origin service.
	req.URL.Host = o.URL.Host
	req.URL.Scheme = o.URL.Scheme
	return o.transport.RoundTrip(req)
}

func (o *localService) staticHost() string {

	addPortIfMissing := func(uri *url.URL, port int) string {
		if uri.Port() != "" {
			return uri.Host
		}
		return fmt.Sprintf("%s:%d", uri.Hostname(), port)
	}

	switch o.URL.Scheme {
	case "ssh":
		return addPortIfMissing(o.URL, 22)
	case "rdp":
		return addPortIfMissing(o.URL, 3389)
	case "smb":
		return addPortIfMissing(o.URL, 445)
	case "tcp":
		return addPortIfMissing(o.URL, 7864) // just a random port since there isn't a default in this case
	}
	return ""

}

// HelloWorld is an OriginService for the built-in Hello World server.
// Users only use this for testing and experimenting with cloudflared.
type helloWorld struct {
	server    net.Listener
	transport *http.Transport
}

func (o *helloWorld) String() string {
	return "Hello World test origin"
}

// Start starts a HelloWorld server and stores its address in the Service receiver.
func (o *helloWorld) start(wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	transport, err := newHTTPTransport(o, cfg)
	if err != nil {
		return err
	}
	o.transport = transport
	helloListener, err := hello.CreateTLSListener("127.0.0.1:")
	if err != nil {
		return errors.Wrap(err, "Cannot start Hello World Server")
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = hello.StartHelloWorldServer(log, helloListener, shutdownC)
	}()
	o.server = helloListener
	return nil
}

func (o *helloWorld) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the request URL so that it goes to the Hello World server.
	req.URL.Host = o.server.Addr().String()
	req.URL.Scheme = "https"
	return o.transport.RoundTrip(req)
}

func (o *helloWorld) Dial(reqURL *url.URL, headers http.Header) (*gws.Conn, *http.Response, error) {
	d := &gws.Dialer{
		TLSClientConfig: o.transport.TLSClientConfig,
	}
	reqURL.Host = o.server.Addr().String()
	reqURL.Scheme = "wss"
	return d.Dial(reqURL.String(), headers)
}

func originRequiresProxy(staticHost string, cfg OriginRequestConfig) bool {
	return staticHost != "" || cfg.BastionMode
}

// statusCode is an OriginService that just responds with a given HTTP status.
// Typical use-case is "user wants the catch-all rule to just respond 404".
type statusCode struct {
	resp *http.Response
}

func newStatusCode(status int) statusCode {
	resp := &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       new(NopReadCloser),
	}
	return statusCode{resp: resp}
}

func (o *statusCode) String() string {
	return fmt.Sprintf("HTTP %d", o.resp.StatusCode)
}

func (o *statusCode) start(wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	return nil
}

func (o *statusCode) RoundTrip(_ *http.Request) (*http.Response, error) {
	return o.resp, nil
}

type NopReadCloser struct{}

// Read always returns EOF to signal end of input
func (nrc *NopReadCloser) Read(buf []byte) (int, error) {
	return 0, io.EOF
}

func (nrc *NopReadCloser) Close() error {
	return nil
}

func newHTTPTransport(service OriginService, cfg OriginRequestConfig) (*http.Transport, error) {
	originCertPool, err := tlsconfig.LoadOriginCA(cfg.CAPool, nil)
	if err != nil {
		return nil, errors.Wrap(err, "Error loading cert pool")
	}

	httpTransport := http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          cfg.KeepAliveConnections,
		MaxIdleConnsPerHost:   cfg.KeepAliveConnections,
		IdleConnTimeout:       cfg.KeepAliveTimeout,
		TLSHandshakeTimeout:   cfg.TLSTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{RootCAs: originCertPool, InsecureSkipVerify: cfg.NoTLSVerify},
	}
	if _, isHelloWorld := service.(*helloWorld); !isHelloWorld && cfg.OriginServerName != "" {
		httpTransport.TLSClientConfig.ServerName = cfg.OriginServerName
	}

	dialer := &net.Dialer{
		Timeout:   cfg.ConnectTimeout,
		KeepAlive: cfg.TCPKeepAlive,
	}
	if cfg.NoHappyEyeballs {
		dialer.FallbackDelay = -1 // As of Golang 1.12, a negative delay disables "happy eyeballs"
	}

	// DialContext depends on which kind of origin is being used.
	dialContext := dialer.DialContext
	switch service := service.(type) {

	// If this origin is a unix socket, enforce network type "unix".
	case *unixSocketPath:
		httpTransport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialContext(ctx, "unix", service.path)
		}

	// Otherwise, use the regular network config.
	default:
		httpTransport.DialContext = dialContext
	}

	return &httpTransport, nil
}
