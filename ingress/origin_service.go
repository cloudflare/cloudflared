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

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/ipaccess"
	"github.com/cloudflare/cloudflared/socks"
	"github.com/cloudflare/cloudflared/tlsconfig"
)

// OriginService is something a tunnel can proxy traffic to.
type OriginService interface {
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

type httpService struct {
	url        *url.URL
	hostHeader string
	transport  *http.Transport
}

func (o *httpService) start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	transport, err := newHTTPTransport(o, cfg, log)
	if err != nil {
		return err
	}
	o.hostHeader = cfg.HTTPHostHeader
	o.transport = transport
	return nil
}

func (o *httpService) String() string {
	return o.url.String()
}

// rawTCPService dials TCP to the destination specified by the client
// It's used by warp routing
type rawTCPService struct {
	name string
}

func (o *rawTCPService) String() string {
	return o.name
}

func (o *rawTCPService) start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	return nil
}

// tcpOverWSService models TCP origins serving eyeballs connecting over websocket, such as
// cloudflared access commands.
type tcpOverWSService struct {
	dest          string
	isBastion     bool
	streamHandler streamHandlerFunc
}

type socksProxyOverWSService struct {
	conn *socksProxyOverWSConnection
}

func newTCPOverWSService(url *url.URL) *tcpOverWSService {
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
	return &tcpOverWSService{
		dest: url.Host,
	}
}

func newBastionService() *tcpOverWSService {
	return &tcpOverWSService{
		isBastion: true,
	}
}

func newSocksProxyOverWSService(accessPolicy *ipaccess.Policy) *socksProxyOverWSService {
	proxy := socksProxyOverWSService{
		conn: &socksProxyOverWSConnection{
			accessPolicy: accessPolicy,
		},
	}

	return &proxy
}

func addPortIfMissing(uri *url.URL, port int) {
	if uri.Port() == "" {
		uri.Host = fmt.Sprintf("%s:%d", uri.Hostname(), port)
	}
}

func (o *tcpOverWSService) String() string {
	if o.isBastion {
		return ServiceBastion
	}
	return o.dest
}

func (o *tcpOverWSService) start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	if cfg.ProxyType == socksProxy {
		o.streamHandler = socks.StreamHandler
	} else {
		o.streamHandler = DefaultStreamHandler
	}
	return nil
}

func (o *socksProxyOverWSService) start(wg *sync.WaitGroup, log *zerolog.Logger, shutdownC <-chan struct{}, errC chan error, cfg OriginRequestConfig) error {
	return nil
}

func (o *socksProxyOverWSService) String() string {
	return ServiceSocksProxy
}

// HelloWorld is an OriginService for the built-in Hello World server.
// Users only use this for testing and experimenting with cloudflared.
type helloWorld struct {
	httpService
	server net.Listener
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
	if err := o.httpService.start(wg, log, shutdownC, errC, cfg); err != nil {
		return err
	}

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

	o.httpService.url = &url.URL{
		Scheme: "https",
		Host:   o.server.Addr().String(),
	}

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

func newHTTPTransport(service OriginService, cfg OriginRequestConfig, log *zerolog.Logger) (*http.Transport, error) {
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
