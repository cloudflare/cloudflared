package ingress

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/socks"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/websocket"
	gws "github.com/gorilla/websocket"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

// originService is something a tunnel can proxy traffic to.
type originService interface {
	String() string
	// Start the origin service if it's managed by cloudflared, e.g. proxy servers or Hello World.
	// If it's not managed by cloudflared, this is a no-op because the user is responsible for
	// starting the origin service.
	start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error
}

// unixSocketPath is an OriginService representing a unix socket (which accepts HTTP)
type unixSocketPath struct {
	path      string
	transport *http.Transport
}

func (o *unixSocketPath) String() string {
	return "unix socket: " + o.path
}

func (o *unixSocketPath) start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	transport, err := newHTTPTransport(o, cfg, log)
	if err != nil {
		return err
	}
	o.transport = transport
	return nil
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

type httpService struct {
	url       *url.URL
	transport *http.Transport
}

func (o *httpService) start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	transport, err := newHTTPTransport(o, cfg, log)
	if err != nil {
		return err
	}
	o.transport = transport
	return nil
}

func (o *httpService) String() string {
	return o.url.String()
}

// bridgeService is like a jump host, the destination is specified by the client
type bridgeService struct {
	client      *tcpClient
	serviceName string
}

// if streamHandler is nil, a default one is set.
func newBridgeService(streamHandler streamHandlerFunc, serviceName string) *bridgeService {
	return &bridgeService{
		client: &tcpClient{
			streamHandler: streamHandler,
		},
		serviceName: serviceName,
	}
}

func (o *bridgeService) String() string {
	return ServiceBridge + ":" + o.serviceName
}

func (o *bridgeService) start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	// streamHandler is already set by the constructor.
	if o.client.streamHandler != nil {
		return nil
	}

	if cfg.ProxyType == socksProxy {
		o.client.streamHandler = socks.StreamHandler
	} else {
		o.client.streamHandler = DefaultStreamHandler
	}
	return nil
}

type singleTCPService struct {
	dest   string
	client *tcpClient
}

func newSingleTCPService(url *url.URL) *singleTCPService {
	switch url.Scheme {
	case "ssh":
		addPortIfMissing(url, 22)
	case "rdp":
		addPortIfMissing(url, 3389)
	case "smb":
		addPortIfMissing(url, 445)
	case "tcp":
		addPortIfMissing(url, 7864) // just a random port since there isn't a default in this case
	}
	return &singleTCPService{
		dest:   url.Host,
		client: &tcpClient{},
	}
}

func addPortIfMissing(uri *url.URL, port int) {
	if uri.Port() == "" {
		uri.Host = fmt.Sprintf("%s:%d", uri.Hostname(), port)
	}
}

func (o *singleTCPService) String() string {
	return o.dest
}

func (o *singleTCPService) start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	if cfg.ProxyType == socksProxy {
		o.client.streamHandler = socks.StreamHandler
	} else {
		o.client.streamHandler = DefaultStreamHandler
	}
	return nil
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
func (o *helloWorld) start(
	wg *sync.WaitGroup,
	log *zerolog.Logger,
	shutdownC <-chan struct{},
	errC chan error,
	cfg OriginRequestConfig,
) error {
	transport, err := newHTTPTransport(o, cfg, log)
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

func (o *statusCode) start(
	wg *sync.WaitGroup,
	log *zerolog.Logger,
	shutdownC <-chan struct{},
	errC chan error,
	cfg OriginRequestConfig,
) error {
	return nil
}

type NopReadCloser struct{}

// Read always returns EOF to signal end of input
func (nrc *NopReadCloser) Read(buf []byte) (int, error) {
	return 0, io.EOF
}

func (nrc *NopReadCloser) Close() error {
	return nil
}

func newHTTPTransport(service originService, cfg OriginRequestConfig, log *zerolog.Logger) (*http.Transport, error) {
	originCertPool, err := tlsconfig.LoadOriginCA(cfg.CAPool, log)
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

// MockOriginHTTPService should only be used by other packages to mock OriginService. Set Transport to configure desired RoundTripper behavior.
type MockOriginHTTPService struct {
	Transport http.RoundTripper
}

func (mos MockOriginHTTPService) RoundTrip(req *http.Request) (*http.Response, error) {
	return mos.Transport.RoundTrip(req)
}

func (mos MockOriginHTTPService) String() string {
	return "MockOriginService"
}

func (mos MockOriginHTTPService) start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	return nil
}
