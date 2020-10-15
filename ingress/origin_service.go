package ingress

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/socks"
	"github.com/cloudflare/cloudflared/websocket"
	"github.com/pkg/errors"
)

// OriginService is something a tunnel can proxy traffic to.
type OriginService interface {
	Address() string
	// Start the origin service if it's managed by cloudflared, e.g. proxy servers or Hello World.
	// If it's not managed by cloudflared, this is a no-op because the user is responsible for
	// starting the origin service.
	Start(wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error
	String() string
	// RewriteOriginURL modifies the HTTP request from cloudflared to the origin, so that it apply
	// this particular type of origin service's specific routing logic.
	RewriteOriginURL(*url.URL)
}

// UnixSocketPath is an OriginService representing a unix socket (which accepts HTTP)
type UnixSocketPath string

func (o UnixSocketPath) Address() string {
	return string(o)
}

func (o UnixSocketPath) String() string {
	return "unix socket: " + string(o)
}

func (o UnixSocketPath) Start(wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	return nil
}

func (o UnixSocketPath) RewriteOriginURL(u *url.URL) {
	// No changes necessary because the origin request URL isn't used.
	// Instead, HTTPTransport's dial is already configured to address the unix socket.
}

// URL is an OriginService listening on a TCP address
type URL struct {
	// The URL for the user's origin service
	RootURL *url.URL
	// The URL that cloudflared should send requests to.
	// If this origin requires starting a proxy, this is the proxy's address,
	// and that proxy points to RootURL. Otherwise, this is equal to RootURL.
	URL *url.URL
}

func (o *URL) Address() string {
	return o.URL.String()
}

func (o *URL) Start(wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	staticHost := o.staticHost()
	if !originRequiresProxy(staticHost, cfg) {
		return nil
	}

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

func (o *URL) String() string {
	return o.Address()
}

func (o *URL) RewriteOriginURL(u *url.URL) {
	u.Host = o.URL.Host
	u.Scheme = o.URL.Scheme
}

func (o *URL) staticHost() string {

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

// HelloWorld is the built-in Hello World service. Used for testing and experimenting with cloudflared.
type HelloWorld struct {
	server net.Listener
}

func (o *HelloWorld) Address() string {
	return o.server.Addr().String()
}

func (o *HelloWorld) String() string {
	return "Hello World static HTML service"
}

// Start starts a HelloWorld server and stores its address in the Service receiver.
func (o *HelloWorld) Start(wg *sync.WaitGroup, log logger.Service, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
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

func (o *HelloWorld) RewriteOriginURL(u *url.URL) {
	u.Host = o.Address()
	u.Scheme = "https"
}

func originRequiresProxy(staticHost string, cfg OriginRequestConfig) bool {
	return staticHost != "" || cfg.BastionMode
}
