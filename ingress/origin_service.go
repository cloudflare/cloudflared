package ingress

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/proxy"

	"github.com/cloudflare/cloudflared/hello"
	"github.com/cloudflare/cloudflared/ipaccess"
	"github.com/cloudflare/cloudflared/management"
	"github.com/cloudflare/cloudflared/socks"
	"github.com/cloudflare/cloudflared/tlsconfig"
)

const (
	HelloWorldService = "hello_world"
	HelloWorldFlag    = "hello-world"
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
	url            *url.URL
	hostHeader     string
	transport      *http.Transport
	matchSNIToHost bool
}

func (o *httpService) start(log *zerolog.Logger, _ <-chan struct{}, cfg OriginRequestConfig) error {
	transport, err := newHTTPTransport(o, cfg, log)
	if err != nil {
		return err
	}
	o.hostHeader = cfg.HTTPHostHeader
	o.transport = transport
	o.matchSNIToHost = cfg.MatchSNIToHost
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
	name         string
	dialer       proxy.Dialer
	writeTimeout time.Duration
	logger       *zerolog.Logger
}

func (o *rawTCPService) String() string {
	return o.name
}

func (o *rawTCPService) start(_ *zerolog.Logger, _ <-chan struct{}, _ OriginRequestConfig) error {
	return nil
}

func (o rawTCPService) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

// proxyAwareDialer wraps net.Dialer with proxy support for both HTTP CONNECT and SOCKS
type proxyAwareDialer struct {
	baseDialer *net.Dialer
	logger     *zerolog.Logger
}

// newProxyAwareDialer creates a dialer that supports proxy settings from environment
func newProxyAwareDialer(timeout, keepAlive time.Duration, logger *zerolog.Logger) proxy.Dialer {
	baseDialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: keepAlive,
	}

	// Check for SOCKS proxy first using standard proxy package
	if socksDialer := proxy.FromEnvironmentUsing(baseDialer); socksDialer != baseDialer {
		if logger != nil {
			logger.Debug().Msg("proxy: using SOCKS proxy from environment")
		}
		return socksDialer
	}

	// Check for HTTP proxy environment variables
	httpProxy := getEnvProxy("HTTP_PROXY", "http_proxy")
	httpsProxy := getEnvProxy("HTTPS_PROXY", "https_proxy")

	if httpProxy == "" && httpsProxy == "" {
		if logger != nil {
			logger.Debug().Msg("proxy: no proxy configured, using direct connection")
		}
		return baseDialer
	}

	if logger != nil {
		logger.Debug().Str("HTTP_PROXY", httpProxy).Str("HTTPS_PROXY", httpsProxy).Msg("proxy: using HTTP proxy from environment")
	}
	return &proxyAwareDialer{
		baseDialer: baseDialer,
		logger:     logger,
	}
}

func getEnvProxy(upper, lower string) string {
	if v := os.Getenv(upper); v != "" {
		return v
	}
	return os.Getenv(lower)
}

func (p *proxyAwareDialer) Dial(network, addr string) (net.Conn, error) {
	return p.DialContext(context.Background(), network, addr)
}

func (p *proxyAwareDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" {
		return p.baseDialer.DialContext(ctx, network, addr)
	}

	req := &http.Request{URL: &url.URL{Scheme: "http", Host: addr}}
	proxyURL, err := http.ProxyFromEnvironment(req)
	if err != nil || proxyURL == nil {
		if p.logger != nil {
			p.logger.Debug().Str("addr", addr).Msg("proxy: direct connection to")
		}
		return p.baseDialer.DialContext(ctx, network, addr)
	}

	if p.logger != nil {
		p.logger.Debug().Str("proxy_url", proxyURL.String()).Str("addr", addr).Msg("proxy: using proxy")
	}

	switch proxyURL.Scheme {
	case "socks4", "socks5":
		return p.dialSOCKS(ctx, proxyURL, network, addr)
	case "http", "https":
		return p.dialHTTPConnect(ctx, proxyURL, addr)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
}

func (p *proxyAwareDialer) dialSOCKS(ctx context.Context, proxyURL *url.URL, network, addr string) (net.Conn, error) {
	socksDialer, err := proxy.FromURL(proxyURL, p.baseDialer)
	if err != nil {
		return nil, fmt.Errorf("SOCKS proxy error: %w", err)
	}

	if contextDialer, ok := socksDialer.(proxy.ContextDialer); ok {
		return contextDialer.DialContext(ctx, network, addr)
	}
	return socksDialer.Dial(network, addr)
}

func (p *proxyAwareDialer) dialHTTPConnect(ctx context.Context, proxyURL *url.URL, addr string) (net.Conn, error) {
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		if proxyURL.Scheme == "https" {
			proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "443")
		} else {
			proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "80")
		}
	}

	conn, err := p.baseDialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("proxy connection failed: %w", err)
	}

	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", addr, addr)
	if _, err := conn.Write([]byte(connectReq)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("CONNECT request failed: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("CONNECT response failed: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("proxy CONNECT failed: %s", resp.Status)
	}

	if p.logger != nil {
		p.logger.Debug().Str("addr", addr).Msg("proxy: HTTP CONNECT successful")
	}
	return conn, nil
}

// tcpOverWSService models TCP origins serving eyeballs connecting over websocket, such as
type tcpOverWSService struct {
	scheme        string
	dest          string
	isBastion     bool
	streamHandler streamHandlerFunc
	dialer        proxy.Dialer
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
		dialer: newProxyAwareDialer(30*time.Second, 30*time.Second, nil),
	}
}

func newBastionService() *tcpOverWSService {
	return &tcpOverWSService{
		isBastion: true,
		dialer:    newProxyAwareDialer(30*time.Second, 30*time.Second, nil),
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
	hostname := uri.Hostname()

	if uri.Port() == "" {
		uri.Host = net.JoinHostPort(hostname, strconv.FormatInt(int64(port), 10))
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
	// Recreate dialer with new timeout and keepalive settings
	o.dialer = newProxyAwareDialer(cfg.ConnectTimeout.Duration, cfg.TCPKeepAlive.Duration, log)
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

	// Set only when the user has not defined any ingress rules
	defaultResp bool
	log         *zerolog.Logger
}

func newStatusCode(status int) statusCode {
	return statusCode{code: status}
}

// default status code (503) that is returned for requests to cloudflared that don't have any ingress rules setup
func newDefaultStatusCode(log *zerolog.Logger) statusCode {
	return statusCode{code: 503, defaultResp: true, log: log}
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

// WarpRoutingService starts a tcp stream between the origin and requests from
// warp clients.
type WarpRoutingService struct {
	Proxy StreamBasedOriginProxy
}

func NewWarpRoutingService(config WarpRoutingConfig, writeTimeout time.Duration) *WarpRoutingService {
	svc := &rawTCPService{
		name:         ServiceWarpRouting,
		dialer:       newProxyAwareDialer(config.ConnectTimeout.Duration, config.TCPKeepAlive.Duration, nil),
		writeTimeout: writeTimeout,
	}

	return &WarpRoutingService{Proxy: svc}
}

// ManagementService starts a local HTTP server to handle incoming management requests.
type ManagementService struct {
	HTTPLocalProxy
}

func newManagementService(managementProxy HTTPLocalProxy) *ManagementService {
	return &ManagementService{
		HTTPLocalProxy: managementProxy,
	}
}

func (o *ManagementService) start(log *zerolog.Logger, _ <-chan struct{}, cfg OriginRequestConfig) error {
	return nil
}

func (o *ManagementService) String() string {
	return "management"
}

func (o ManagementService) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.String())
}

func NewManagementRule(management *management.ManagementService) Rule {
	return Rule{
		Hostname: management.Hostname,
		Service:  newManagementService(management),
	}
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
