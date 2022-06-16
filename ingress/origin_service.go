package ingress

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/ipaccess"
	"github.com/cloudflare/cloudflared/socks"
	"github.com/cloudflare/cloudflared/tlsconfig"
)

const (
	HelloWorldService = "hello_world"
	HttpStatusService = "http_status"
)

// OriginService is something a tunnel can proxy traffic to.
type OriginService interface {
	String() string
	// Start the origin service if it's managed by cloudflared, e.g. proxy servers or Hello World.
	// If it's not managed by cloudflared, this is a no-op because the user is responsible for
	// starting the origin service.
	// Implementor of services managed by cloudflared should terminate the service if shutdownC is closed
	start(log *zerolog.Logger, shutdownC <-chan struct{}, cfg OriginRequestConfig) error
	MarshalJSON() ([]byte, error)
}

// unixSocketPath is an OriginService representing a unix socket (which accepts HTTP or HTTPS)
type unixSocketPath struct {
	path      string
	scheme    string
	transport *http.Transport
}

func (o *unixSocketPath) String() string {
	scheme := ""
	if o.scheme == "https" {
		scheme = "+tls"
	}
	return fmt.Sprintf("unix%s:%s", scheme, o.path)
}

func (o *unixSocketPath) start(log *zerolog.Logger, _ <-chan struct{}, cfg OriginRequestConfig) error {
	transport, err := newHTTPTransport(o, cfg, log)
	if err != nil {
		return err
	}
	o.transport = transport
	return nil
}

func (o unixSocketPath) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

type httpService struct {
	url        *url.URL
	hostHeader string
	transport  *http.Transport
}

func (o *httpService) start(log *zerolog.Logger, _ <-chan struct{}, cfg OriginRequestConfig) error {
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

func (o httpService) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// rawTCPService dials TCP to the destination specified by the client
// It's used by warp routing
type rawTCPService struct {
	name   string
	dialer net.Dialer
}

func (o *rawTCPService) String() string {
	return o.name
}

func (o *rawTCPService) start(log *zerolog.Logger, _ <-chan struct{}, cfg OriginRequestConfig) error {
	return nil
}

func (o rawTCPService) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// tcpOverWSService models TCP origins serving eyeballs connecting over websocket, such as
// cloudflared access commands.
type tcpOverWSService struct {
	scheme        string
	dest          string
	isBastion     bool
	streamHandler streamHandlerFunc
	dialer        net.Dialer
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
		scheme: url.Scheme,
		dest:   url.Host,
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

	if o.scheme != "" {
		return fmt.Sprintf("%s://%s", o.scheme, o.dest)
	} else {
		return o.dest
	}
}

func (o *tcpOverWSService) start(log *zerolog.Logger, _ <-chan struct{}, cfg OriginRequestConfig) error {
	if cfg.ProxyType == socksProxy {
		o.streamHandler = socks.StreamHandler
	} else {
		o.streamHandler = DefaultStreamHandler
	}
	o.dialer.Timeout = cfg.ConnectTimeout.Duration
	o.dialer.KeepAlive = cfg.TCPKeepAlive.Duration
	return nil
}

func (o tcpOverWSService) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

func (o *socksProxyOverWSService) start(log *zerolog.Logger, _ <-chan struct{}, cfg OriginRequestConfig) error {
	return nil
}

func (o *socksProxyOverWSService) String() string {
	return ServiceSocksProxy
}

func (o socksProxyOverWSService) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// HelloWorld is an OriginService for the built-in Hello World server.
// Users only use this for testing and experimenting with cloudflared.
type helloWorld struct {
	httpService
	server net.Listener
}

func (o *helloWorld) String() string {
	return HelloWorldService
}

// Start starts a HelloWorld server and stores its address in the Service receiver.
func (o *helloWorld) start(
	log *zerolog.Logger,
	shutdownC <-chan struct{},
	cfg OriginRequestConfig,
) error {
	if err := o.httpService.start(log, shutdownC, cfg); err != nil {
		return err
	}

	helloListener, err := hello.CreateTLSListener("127.0.0.1:")
	if err != nil {
		return errors.Wrap(err, "Cannot start Hello World Server")
	}
	go hello.StartHelloWorldServer(log, helloListener, shutdownC)
	o.server = helloListener

	o.httpService.url = &url.URL{
		Scheme: "https",
		Host:   o.server.Addr().String(),
	}

	return nil
}

func (o helloWorld) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// statusCode is an OriginService that just responds with a given HTTP status.
// Typical use-case is "user wants the catch-all rule to just respond 404".
type statusCode struct {
	code int
}

func newStatusCode(status int) statusCode {
	return statusCode{code: status}
}

func (o *statusCode) String() string {
	return fmt.Sprintf("http_status:%d", o.code)
}

func (o *statusCode) start(
	log *zerolog.Logger,
	_ <-chan struct{},
	cfg OriginRequestConfig,
) error {
	return nil
}

func (o statusCode) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
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
		IdleConnTimeout:       cfg.KeepAliveTimeout.Duration,
		TLSHandshakeTimeout:   cfg.TLSTimeout.Duration,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{RootCAs: originCertPool, InsecureSkipVerify: cfg.NoTLSVerify},
		ForceAttemptHTTP2:     cfg.Http2Origin,
	}
	if _, isHelloWorld := service.(*helloWorld); !isHelloWorld && cfg.OriginServerName != "" {
		httpTransport.TLSClientConfig.ServerName = cfg.OriginServerName
	}

	dialer := &net.Dialer{
		Timeout:   cfg.ConnectTimeout.Duration,
		KeepAlive: cfg.TCPKeepAlive.Duration,
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

func (mos MockOriginHTTPService) start(log *zerolog.Logger, _ <-chan struct{}, cfg OriginRequestConfig) error {
	return nil
}

func (mos MockOriginHTTPService) MarshalJSON() ([]byte, error) {
	return json.Marshal(mos.String())
}
