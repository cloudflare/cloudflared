package pogs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudflare/cloudflared/originservice"
	"github.com/cloudflare/cloudflared/tlsconfig"
	"github.com/cloudflare/cloudflared/tunnelrpc"
	"github.com/pkg/errors"
	capnp "zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/pogs"
	"zombiezen.com/go/capnproto2/rpc"
)

///
/// Structs
///

type ClientConfig struct {
	Version                uint64
	AutoUpdateFrequency    time.Duration
	MetricsUpdateFrequency time.Duration
	HeartbeatInterval      time.Duration
	MaxFailedHeartbeats    uint64
	GracePeriod            time.Duration
	DoHProxyConfigs        []*DoHProxyConfig
	ReverseProxyConfigs    []*ReverseProxyConfig
	NumHAConnections       uint8
}

type UseConfigurationResult struct {
	Success      bool
	ErrorMessage string
}

type DoHProxyConfig struct {
	ListenHost string
	ListenPort uint16
	Upstreams  []string
}

type ReverseProxyConfig struct {
	TunnelHostname     string
	Origin             OriginConfig
	Retries            uint64
	ConnectionTimeout  time.Duration
	CompressionQuality uint64
}

func NewReverseProxyConfig(
	tunnelHostname string,
	originConfig OriginConfig,
	retries uint64,
	connectionTimeout time.Duration,
	compressionQuality uint64,
) (*ReverseProxyConfig, error) {
	if originConfig == nil {
		return nil, fmt.Errorf("NewReverseProxyConfig: originConfig was null")
	}
	return &ReverseProxyConfig{
		TunnelHostname:     tunnelHostname,
		Origin:             originConfig,
		Retries:            retries,
		ConnectionTimeout:  connectionTimeout,
		CompressionQuality: compressionQuality,
	}, nil
}

//go-sumtype:decl OriginConfig
type OriginConfig interface {
	// Service returns a OriginService used to proxy to the origin
	Service() (originservice.OriginService, error)
	// go-sumtype requires at least one unexported method, otherwise it will complain that interface is not sealed
	isOriginConfig()
}

type HTTPOriginConfig struct {
	URL                   OriginAddr    `capnp:"url"`
	TCPKeepAlive          time.Duration `capnp:"tcpKeepAlive"`
	DialDualStack         bool
	TLSHandshakeTimeout   time.Duration `capnp:"tlsHandshakeTimeout"`
	TLSVerify             bool          `capnp:"tlsVerify"`
	OriginCAPool          string
	OriginServerName      string
	MaxIdleConnections    uint64
	IdleConnectionTimeout time.Duration
	ProxyConnectTimeout   time.Duration
	ExpectContinueTimeout time.Duration
	ChunkedEncoding       bool
}

type OriginAddr interface {
	Addr() string
}

type HTTPURL struct {
	URL *url.URL
}

func (ha *HTTPURL) Addr() string {
	return ha.URL.String()
}

func (ha *HTTPURL) capnpHTTPURL() *CapnpHTTPURL {
	return &CapnpHTTPURL{
		URL: ha.URL.String(),
	}
}

// URL for a HTTP origin, capnp doesn't have native support for URL, so represent it as string
type CapnpHTTPURL struct {
	URL string `capnp:"url"`
}

type UnixPath struct {
	Path string
}

func (up *UnixPath) Addr() string {
	return up.Path
}

func (hc *HTTPOriginConfig) Service() (originservice.OriginService, error) {
	rootCAs, err := tlsconfig.LoadCustomCertPool(hc.OriginCAPool)
	if err != nil {
		return nil, err
	}
	dialContext := (&net.Dialer{
		Timeout:   hc.ProxyConnectTimeout,
		KeepAlive: hc.TCPKeepAlive,
		DualStack: hc.DialDualStack,
	}).DialContext
	transport := &http.Transport{
		Proxy:       http.ProxyFromEnvironment,
		DialContext: dialContext,
		TLSClientConfig: &tls.Config{
			RootCAs:            rootCAs,
			ServerName:         hc.OriginServerName,
			InsecureSkipVerify: hc.TLSVerify,
		},
		TLSHandshakeTimeout:   hc.TLSHandshakeTimeout,
		MaxIdleConns:          int(hc.MaxIdleConnections),
		IdleConnTimeout:       hc.IdleConnectionTimeout,
		ExpectContinueTimeout: hc.ExpectContinueTimeout,
	}
	if unixPath, ok := hc.URL.(*UnixPath); ok {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialContext(ctx, "unix", unixPath.Addr())
		}
	}
	return originservice.NewHTTPService(transport, hc.URL.Addr(), hc.ChunkedEncoding), nil
}

func (_ *HTTPOriginConfig) isOriginConfig() {}

type WebSocketOriginConfig struct {
	URL              string `capnp:"url"`
	TLSVerify        bool   `capnp:"tlsVerify"`
	OriginCAPool     string
	OriginServerName string
}

func (wsc *WebSocketOriginConfig) Service() (originservice.OriginService, error) {
	rootCAs, err := tlsconfig.LoadCustomCertPool(wsc.OriginCAPool)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{
		RootCAs:            rootCAs,
		ServerName:         wsc.OriginServerName,
		InsecureSkipVerify: wsc.TLSVerify,
	}
	return originservice.NewWebSocketService(tlsConfig, wsc.URL)
}

func (_ *WebSocketOriginConfig) isOriginConfig() {}

type HelloWorldOriginConfig struct{}

func (_ *HelloWorldOriginConfig) Service() (originservice.OriginService, error) {
	helloCert, err := tlsconfig.GetHelloCertificateX509()
	if err != nil {
		return nil, errors.Wrap(err, "Cannot get Hello World server certificate")
	}
	rootCAs := x509.NewCertPool()
	rootCAs.AddCert(helloCert)
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			RootCAs: rootCAs,
		},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return originservice.NewHelloWorldService(transport)
}

func (_ *HelloWorldOriginConfig) isOriginConfig() {}

/*
 * Boilerplate to convert between these structs and the primitive structs
 * generated by capnp-go.
 * Mnemonics for variable names in this section:
 *   - `p` is for POGS (plain old Go struct)
 *   - `s` (and `ss`) is for "capnp.Struct", which is the fundamental type
 *     underlying the capnp-go data structures.
 */

func MarshalClientConfig(s tunnelrpc.ClientConfig, p *ClientConfig) error {
	s.SetVersion(p.Version)
	s.SetAutoUpdateFrequency(p.AutoUpdateFrequency.Nanoseconds())
	s.SetMetricsUpdateFrequency(p.MetricsUpdateFrequency.Nanoseconds())
	s.SetHeartbeatInterval(p.HeartbeatInterval.Nanoseconds())
	s.SetMaxFailedHeartbeats(p.MaxFailedHeartbeats)
	s.SetGracePeriod(p.GracePeriod.Nanoseconds())
	s.SetNumHAConnections(p.NumHAConnections)
	err := marshalDoHProxyConfigs(s, p.DoHProxyConfigs)
	if err != nil {
		return err
	}
	return marshalReverseProxyConfigs(s, p.ReverseProxyConfigs)
}

func marshalDoHProxyConfigs(s tunnelrpc.ClientConfig, dohProxyConfigs []*DoHProxyConfig) error {
	capnpList, err := s.NewDohProxyConfigs(int32(len(dohProxyConfigs)))
	if err != nil {
		return err
	}
	for i, unmarshalledConfig := range dohProxyConfigs {
		err := MarshalDoHProxyConfig(capnpList.At(i), unmarshalledConfig)
		if err != nil {
			return err
		}
	}
	return nil
}

func marshalReverseProxyConfigs(s tunnelrpc.ClientConfig, reverseProxyConfigs []*ReverseProxyConfig) error {
	capnpList, err := s.NewReverseProxyConfigs(int32(len(reverseProxyConfigs)))
	if err != nil {
		return err
	}
	for i, unmarshalledConfig := range reverseProxyConfigs {
		err := MarshalReverseProxyConfig(capnpList.At(i), unmarshalledConfig)
		if err != nil {
			return err
		}
	}
	return nil
}

func UnmarshalClientConfig(s tunnelrpc.ClientConfig) (*ClientConfig, error) {
	p := new(ClientConfig)
	p.Version = s.Version()
	p.AutoUpdateFrequency = time.Duration(s.AutoUpdateFrequency())
	p.MetricsUpdateFrequency = time.Duration(s.MetricsUpdateFrequency())
	p.HeartbeatInterval = time.Duration(s.HeartbeatInterval())
	p.MaxFailedHeartbeats = s.MaxFailedHeartbeats()
	p.GracePeriod = time.Duration(s.GracePeriod())
	p.NumHAConnections = s.NumHAConnections()
	dohProxyConfigs, err := unmarshalDoHProxyConfigs(s)
	if err != nil {
		return nil, err
	}
	p.DoHProxyConfigs = dohProxyConfigs
	reverseProxyConfigs, err := unmarshalReverseProxyConfigs(s)
	if err != nil {
		return nil, err
	}
	p.ReverseProxyConfigs = reverseProxyConfigs
	return p, err
}

func unmarshalDoHProxyConfigs(s tunnelrpc.ClientConfig) ([]*DoHProxyConfig, error) {
	var result []*DoHProxyConfig
	marshalledDoHProxyConfigs, err := s.DohProxyConfigs()
	if err != nil {
		return nil, err
	}
	for i := 0; i < marshalledDoHProxyConfigs.Len(); i++ {
		ss := marshalledDoHProxyConfigs.At(i)
		dohProxyConfig, err := UnmarshalDoHProxyConfig(ss)
		if err != nil {
			return nil, err
		}
		result = append(result, dohProxyConfig)
	}
	return result, nil
}

func unmarshalReverseProxyConfigs(s tunnelrpc.ClientConfig) ([]*ReverseProxyConfig, error) {
	var result []*ReverseProxyConfig
	marshalledReverseProxyConfigs, err := s.ReverseProxyConfigs()
	if err != nil {
		return nil, err
	}
	for i := 0; i < marshalledReverseProxyConfigs.Len(); i++ {
		ss := marshalledReverseProxyConfigs.At(i)
		reverseProxyConfig, err := UnmarshalReverseProxyConfig(ss)
		if err != nil {
			return nil, err
		}
		result = append(result, reverseProxyConfig)
	}
	return result, nil
}

func MarshalUseConfigurationResult(s tunnelrpc.UseConfigurationResult, p *UseConfigurationResult) error {
	return pogs.Insert(tunnelrpc.UseConfigurationResult_TypeID, s.Struct, p)
}

func UnmarshalUseConfigurationResult(s tunnelrpc.UseConfigurationResult) (*UseConfigurationResult, error) {
	p := new(UseConfigurationResult)
	err := pogs.Extract(p, tunnelrpc.UseConfigurationResult_TypeID, s.Struct)
	return p, err
}

func MarshalDoHProxyConfig(s tunnelrpc.DoHProxyConfig, p *DoHProxyConfig) error {
	return pogs.Insert(tunnelrpc.DoHProxyConfig_TypeID, s.Struct, p)
}

func UnmarshalDoHProxyConfig(s tunnelrpc.DoHProxyConfig) (*DoHProxyConfig, error) {
	p := new(DoHProxyConfig)
	err := pogs.Extract(p, tunnelrpc.DoHProxyConfig_TypeID, s.Struct)
	return p, err
}

func MarshalReverseProxyConfig(s tunnelrpc.ReverseProxyConfig, p *ReverseProxyConfig) error {
	s.SetTunnelHostname(p.TunnelHostname)
	switch config := p.Origin.(type) {
	case *HTTPOriginConfig:
		ss, err := s.Origin().NewHttp()
		if err != nil {
			return err
		}
		if err := MarshalHTTPOriginConfig(ss, config); err != nil {
			return err
		}
	case *WebSocketOriginConfig:
		ss, err := s.Origin().NewWebsocket()
		if err != nil {
			return err
		}
		if err := MarshalWebSocketOriginConfig(ss, config); err != nil {
			return err
		}
	case *HelloWorldOriginConfig:
		ss, err := s.Origin().NewHelloWorld()
		if err != nil {
			return err
		}
		if err := MarshalHelloWorldOriginConfig(ss, config); err != nil {
			return err
		}
	default:
		return fmt.Errorf("Unknown type for config: %T", config)
	}
	s.SetRetries(p.Retries)
	s.SetConnectionTimeout(p.ConnectionTimeout.Nanoseconds())
	s.SetCompressionQuality(p.CompressionQuality)
	return nil
}

func UnmarshalReverseProxyConfig(s tunnelrpc.ReverseProxyConfig) (*ReverseProxyConfig, error) {
	p := new(ReverseProxyConfig)
	tunnelHostname, err := s.TunnelHostname()
	if err != nil {
		return nil, err
	}
	p.TunnelHostname = tunnelHostname
	switch s.Origin().Which() {
	case tunnelrpc.ReverseProxyConfig_origin_Which_http:
		ss, err := s.Origin().Http()
		if err != nil {
			return nil, err
		}
		config, err := UnmarshalHTTPOriginConfig(ss)
		if err != nil {
			return nil, err
		}
		p.Origin = config
	case tunnelrpc.ReverseProxyConfig_origin_Which_websocket:
		ss, err := s.Origin().Websocket()
		if err != nil {
			return nil, err
		}
		config, err := UnmarshalWebSocketOriginConfig(ss)
		if err != nil {
			return nil, err
		}
		p.Origin = config
	case tunnelrpc.ReverseProxyConfig_origin_Which_helloWorld:
		ss, err := s.Origin().HelloWorld()
		if err != nil {
			return nil, err
		}
		config, err := UnmarshalHelloWorldOriginConfig(ss)
		if err != nil {
			return nil, err
		}
		p.Origin = config
	}
	p.Retries = s.Retries()
	p.ConnectionTimeout = time.Duration(s.ConnectionTimeout())
	p.CompressionQuality = s.CompressionQuality()
	return p, nil
}

func MarshalHTTPOriginConfig(s tunnelrpc.HTTPOriginConfig, p *HTTPOriginConfig) error {
	switch originAddr := p.URL.(type) {
	case *HTTPURL:
		ss, err := s.OriginAddr().NewHttp()
		if err != nil {
			return err
		}
		if err := MarshalHTTPURL(ss, originAddr); err != nil {
			return err
		}
	case *UnixPath:
		ss, err := s.OriginAddr().NewUnix()
		if err != nil {
			return err
		}
		if err := MarshalUnixPath(ss, originAddr); err != nil {
			return err
		}
	default:
		return fmt.Errorf("Unknown type for OriginAddr: %T", originAddr)
	}
	s.SetTcpKeepAlive(p.TCPKeepAlive.Nanoseconds())
	s.SetDialDualStack(p.DialDualStack)
	s.SetTlsHandshakeTimeout(p.TLSHandshakeTimeout.Nanoseconds())
	s.SetTlsVerify(p.TLSVerify)
	s.SetOriginCAPool(p.OriginCAPool)
	s.SetOriginServerName(p.OriginServerName)
	s.SetMaxIdleConnections(p.MaxIdleConnections)
	s.SetIdleConnectionTimeout(p.IdleConnectionTimeout.Nanoseconds())
	s.SetProxyConnectionTimeout(p.ProxyConnectTimeout.Nanoseconds())
	s.SetExpectContinueTimeout(p.ExpectContinueTimeout.Nanoseconds())
	s.SetChunkedEncoding(p.ChunkedEncoding)
	return nil
}

func UnmarshalHTTPOriginConfig(s tunnelrpc.HTTPOriginConfig) (*HTTPOriginConfig, error) {
	p := new(HTTPOriginConfig)
	switch s.OriginAddr().Which() {
	case tunnelrpc.HTTPOriginConfig_originAddr_Which_http:
		ss, err := s.OriginAddr().Http()
		if err != nil {
			return nil, err
		}
		originAddr, err := UnmarshalCapnpHTTPURL(ss)
		if err != nil {
			return nil, err
		}
		p.URL = originAddr
	case tunnelrpc.HTTPOriginConfig_originAddr_Which_unix:
		ss, err := s.OriginAddr().Unix()
		if err != nil {
			return nil, err
		}
		originAddr, err := UnmarshalUnixPath(ss)
		if err != nil {
			return nil, err
		}
		p.URL = originAddr
	default:
		return nil, fmt.Errorf("Unknown type for OriginAddr: %T", s.OriginAddr().Which())
	}
	p.TCPKeepAlive = time.Duration(s.TcpKeepAlive())
	p.DialDualStack = s.DialDualStack()
	p.TLSHandshakeTimeout = time.Duration(s.TlsHandshakeTimeout())
	p.TLSVerify = s.TlsVerify()
	originCAPool, err := s.OriginCAPool()
	if err != nil {
		return nil, err
	}
	p.OriginCAPool = originCAPool
	originServerName, err := s.OriginServerName()
	if err != nil {
		return nil, err
	}
	p.OriginServerName = originServerName
	p.MaxIdleConnections = s.MaxIdleConnections()
	p.IdleConnectionTimeout = time.Duration(s.IdleConnectionTimeout())
	p.ProxyConnectTimeout = time.Duration(s.ProxyConnectionTimeout())
	p.ExpectContinueTimeout = time.Duration(s.ExpectContinueTimeout())
	p.ChunkedEncoding = s.ChunkedEncoding()
	return p, nil
}

func MarshalHTTPURL(s tunnelrpc.CapnpHTTPURL, p *HTTPURL) error {
	return pogs.Insert(tunnelrpc.CapnpHTTPURL_TypeID, s.Struct, p.capnpHTTPURL())
}

func UnmarshalCapnpHTTPURL(s tunnelrpc.CapnpHTTPURL) (*HTTPURL, error) {
	p := new(CapnpHTTPURL)
	err := pogs.Extract(p, tunnelrpc.CapnpHTTPURL_TypeID, s.Struct)
	if err != nil {
		return nil, err
	}
	url, err := url.Parse(p.URL)
	if err != nil {
		return nil, err
	}
	return &HTTPURL{
		URL: url,
	}, nil
}

func MarshalUnixPath(s tunnelrpc.UnixPath, p *UnixPath) error {
	err := pogs.Insert(tunnelrpc.UnixPath_TypeID, s.Struct, p)
	return err
}

func UnmarshalUnixPath(s tunnelrpc.UnixPath) (*UnixPath, error) {
	p := new(UnixPath)
	err := pogs.Extract(p, tunnelrpc.UnixPath_TypeID, s.Struct)
	return p, err
}

func MarshalWebSocketOriginConfig(s tunnelrpc.WebSocketOriginConfig, p *WebSocketOriginConfig) error {
	return pogs.Insert(tunnelrpc.WebSocketOriginConfig_TypeID, s.Struct, p)
}

func UnmarshalWebSocketOriginConfig(s tunnelrpc.WebSocketOriginConfig) (*WebSocketOriginConfig, error) {
	p := new(WebSocketOriginConfig)
	err := pogs.Extract(p, tunnelrpc.WebSocketOriginConfig_TypeID, s.Struct)
	return p, err
}

func MarshalHelloWorldOriginConfig(s tunnelrpc.HelloWorldOriginConfig, p *HelloWorldOriginConfig) error {
	return pogs.Insert(tunnelrpc.HelloWorldOriginConfig_TypeID, s.Struct, p)
}

func UnmarshalHelloWorldOriginConfig(s tunnelrpc.HelloWorldOriginConfig) (*HelloWorldOriginConfig, error) {
	p := new(HelloWorldOriginConfig)
	err := pogs.Extract(p, tunnelrpc.HelloWorldOriginConfig_TypeID, s.Struct)
	return p, err
}

type ClientService interface {
	UseConfiguration(ctx context.Context, config *ClientConfig) (*UseConfigurationResult, error)
}

type ClientService_PogsClient struct {
	Client capnp.Client
	Conn   *rpc.Conn
}

func (c *ClientService_PogsClient) Close() error {
	return c.Conn.Close()
}

func (c *ClientService_PogsClient) UseConfiguration(
	ctx context.Context,
	config *ClientConfig,
) (*UseConfigurationResult, error) {
	client := tunnelrpc.ClientService{Client: c.Client}
	promise := client.UseConfiguration(ctx, func(p tunnelrpc.ClientService_useConfiguration_Params) error {
		clientServiceConfig, err := p.NewClientServiceConfig()
		if err != nil {
			return err
		}
		return MarshalClientConfig(clientServiceConfig, config)
	})
	retval, err := promise.Result().Struct()
	if err != nil {
		return nil, err
	}
	return UnmarshalUseConfigurationResult(retval)
}
