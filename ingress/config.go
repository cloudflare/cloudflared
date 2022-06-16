package ingress

import (
	"encoding/json"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ipaccess"
	"github.com/cloudflare/cloudflared/tlsconfig"
)

var (
	defaultHTTPConnectTimeout        = config.CustomDuration{Duration: 30 * time.Second}
	defaultWarpRoutingConnectTimeout = config.CustomDuration{Duration: 5 * time.Second}
	defaultTLSTimeout                = config.CustomDuration{Duration: 10 * time.Second}
	defaultTCPKeepAlive              = config.CustomDuration{Duration: 30 * time.Second}
	defaultKeepAliveTimeout          = config.CustomDuration{Duration: 90 * time.Second}
)

const (
	defaultProxyAddress           = "127.0.0.1"
	defaultKeepAliveConnections   = 100
	SSHServerFlag                 = "ssh-server"
	Socks5Flag                    = "socks5"
	ProxyConnectTimeoutFlag       = "proxy-connect-timeout"
	ProxyTLSTimeoutFlag           = "proxy-tls-timeout"
	ProxyTCPKeepAliveFlag         = "proxy-tcp-keepalive"
	ProxyNoHappyEyeballsFlag      = "proxy-no-happy-eyeballs"
	ProxyKeepAliveConnectionsFlag = "proxy-keepalive-connections"
	ProxyKeepAliveTimeoutFlag     = "proxy-keepalive-timeout"
	HTTPHostHeaderFlag            = "http-host-header"
	OriginServerNameFlag          = "origin-server-name"
	NoTLSVerifyFlag               = "no-tls-verify"
	NoChunkedEncodingFlag         = "no-chunked-encoding"
	ProxyAddressFlag              = "proxy-address"
	ProxyPortFlag                 = "proxy-port"
	Http2OriginFlag               = "http2-origin"
)

const (
	socksProxy = "socks"
)

type WarpRoutingConfig struct {
	Enabled        bool                  `yaml:"enabled" json:"enabled"`
	ConnectTimeout config.CustomDuration `yaml:"connectTimeout" json:"connectTimeout,omitempty"`
	TCPKeepAlive   config.CustomDuration `yaml:"tcpKeepAlive" json:"tcpKeepAlive,omitempty"`
}

func NewWarpRoutingConfig(raw *config.WarpRoutingConfig) WarpRoutingConfig {
	cfg := WarpRoutingConfig{
		Enabled:        raw.Enabled,
		ConnectTimeout: defaultWarpRoutingConnectTimeout,
		TCPKeepAlive:   defaultTCPKeepAlive,
	}
	if raw.ConnectTimeout != nil {
		cfg.ConnectTimeout = *raw.ConnectTimeout
	}
	if raw.TCPKeepAlive != nil {
		cfg.TCPKeepAlive = *raw.TCPKeepAlive
	}
	return cfg
}

func (c *WarpRoutingConfig) RawConfig() config.WarpRoutingConfig {
	raw := config.WarpRoutingConfig{
		Enabled: c.Enabled,
	}
	if c.ConnectTimeout.Duration != defaultWarpRoutingConnectTimeout.Duration {
		raw.ConnectTimeout = &c.ConnectTimeout
	}
	if c.TCPKeepAlive.Duration != defaultTCPKeepAlive.Duration {
		raw.TCPKeepAlive = &c.TCPKeepAlive
	}
	return raw
}

// RemoteConfig models ingress settings that can be managed remotely, for example through the dashboard.
type RemoteConfig struct {
	Ingress     Ingress
	WarpRouting WarpRoutingConfig
}

type RemoteConfigJSON struct {
	GlobalOriginRequest *config.OriginRequestConfig     `json:"originRequest,omitempty"`
	IngressRules        []config.UnvalidatedIngressRule `json:"ingress"`
	WarpRouting         config.WarpRoutingConfig        `json:"warp-routing"`
}

func (rc *RemoteConfig) UnmarshalJSON(b []byte) error {
	var rawConfig RemoteConfigJSON

	if err := json.Unmarshal(b, &rawConfig); err != nil {
		return err
	}

	// if nil, just assume the default values.
	globalOriginRequestConfig := rawConfig.GlobalOriginRequest
	if globalOriginRequestConfig == nil {
		globalOriginRequestConfig = &config.OriginRequestConfig{}
	}

	ingress, err := validateIngress(rawConfig.IngressRules, originRequestFromConfig(*globalOriginRequestConfig))
	if err != nil {
		return err
	}

	rc.Ingress = ingress
	rc.WarpRouting = NewWarpRoutingConfig(&rawConfig.WarpRouting)

	return nil
}

func originRequestFromSingeRule(c *cli.Context) OriginRequestConfig {
	var connectTimeout = defaultHTTPConnectTimeout
	var tlsTimeout = defaultTLSTimeout
	var tcpKeepAlive = defaultTCPKeepAlive
	var noHappyEyeballs bool
	var keepAliveConnections = defaultKeepAliveConnections
	var keepAliveTimeout = defaultKeepAliveTimeout
	var httpHostHeader string
	var originServerName string
	var caPool string
	var noTLSVerify bool
	var disableChunkedEncoding bool
	var bastionMode bool
	var proxyAddress = defaultProxyAddress
	var proxyPort uint
	var proxyType string
	var http2Origin bool
	if flag := ProxyConnectTimeoutFlag; c.IsSet(flag) {
		connectTimeout = config.CustomDuration{Duration: c.Duration(flag)}
	}
	if flag := ProxyTLSTimeoutFlag; c.IsSet(flag) {
		tlsTimeout = config.CustomDuration{Duration: c.Duration(flag)}
	}
	if flag := ProxyTCPKeepAliveFlag; c.IsSet(flag) {
		tcpKeepAlive = config.CustomDuration{Duration: c.Duration(flag)}
	}
	if flag := ProxyNoHappyEyeballsFlag; c.IsSet(flag) {
		noHappyEyeballs = c.Bool(flag)
	}
	if flag := ProxyKeepAliveConnectionsFlag; c.IsSet(flag) {
		keepAliveConnections = c.Int(flag)
	}
	if flag := ProxyKeepAliveTimeoutFlag; c.IsSet(flag) {
		keepAliveTimeout = config.CustomDuration{Duration: c.Duration(flag)}
	}
	if flag := HTTPHostHeaderFlag; c.IsSet(flag) {
		httpHostHeader = c.String(flag)
	}
	if flag := OriginServerNameFlag; c.IsSet(flag) {
		originServerName = c.String(flag)
	}
	if flag := tlsconfig.OriginCAPoolFlag; c.IsSet(flag) {
		caPool = c.String(flag)
	}
	if flag := NoTLSVerifyFlag; c.IsSet(flag) {
		noTLSVerify = c.Bool(flag)
	}
	if flag := NoChunkedEncodingFlag; c.IsSet(flag) {
		disableChunkedEncoding = c.Bool(flag)
	}
	if flag := config.BastionFlag; c.IsSet(flag) {
		bastionMode = c.Bool(flag)
	}
	if flag := ProxyAddressFlag; c.IsSet(flag) {
		proxyAddress = c.String(flag)
	}
	if flag := ProxyPortFlag; c.IsSet(flag) {
		// Note TUN-3758 , we use Int because UInt is not supported with altsrc
		proxyPort = uint(c.Int(flag))
	}
	if flag := Http2OriginFlag; c.IsSet(flag) {
		http2Origin = c.Bool(flag)
	}
	if c.IsSet(Socks5Flag) {
		proxyType = socksProxy
	}

	return OriginRequestConfig{
		ConnectTimeout:         connectTimeout,
		TLSTimeout:             tlsTimeout,
		TCPKeepAlive:           tcpKeepAlive,
		NoHappyEyeballs:        noHappyEyeballs,
		KeepAliveConnections:   keepAliveConnections,
		KeepAliveTimeout:       keepAliveTimeout,
		HTTPHostHeader:         httpHostHeader,
		OriginServerName:       originServerName,
		CAPool:                 caPool,
		NoTLSVerify:            noTLSVerify,
		DisableChunkedEncoding: disableChunkedEncoding,
		BastionMode:            bastionMode,
		ProxyAddress:           proxyAddress,
		ProxyPort:              proxyPort,
		ProxyType:              proxyType,
		Http2Origin:            http2Origin,
	}
}

func originRequestFromConfig(c config.OriginRequestConfig) OriginRequestConfig {
	out := OriginRequestConfig{
		ConnectTimeout:       defaultHTTPConnectTimeout,
		TLSTimeout:           defaultTLSTimeout,
		TCPKeepAlive:         defaultTCPKeepAlive,
		KeepAliveConnections: defaultKeepAliveConnections,
		KeepAliveTimeout:     defaultKeepAliveTimeout,
		ProxyAddress:         defaultProxyAddress,
	}
	if c.ConnectTimeout != nil {
		out.ConnectTimeout = *c.ConnectTimeout
	}
	if c.TLSTimeout != nil {
		out.TLSTimeout = *c.TLSTimeout
	}
	if c.TCPKeepAlive != nil {
		out.TCPKeepAlive = *c.TCPKeepAlive
	}
	if c.NoHappyEyeballs != nil {
		out.NoHappyEyeballs = *c.NoHappyEyeballs
	}
	if c.KeepAliveConnections != nil {
		out.KeepAliveConnections = *c.KeepAliveConnections
	}
	if c.KeepAliveTimeout != nil {
		out.KeepAliveTimeout = *c.KeepAliveTimeout
	}
	if c.HTTPHostHeader != nil {
		out.HTTPHostHeader = *c.HTTPHostHeader
	}
	if c.OriginServerName != nil {
		out.OriginServerName = *c.OriginServerName
	}
	if c.CAPool != nil {
		out.CAPool = *c.CAPool
	}
	if c.NoTLSVerify != nil {
		out.NoTLSVerify = *c.NoTLSVerify
	}
	if c.DisableChunkedEncoding != nil {
		out.DisableChunkedEncoding = *c.DisableChunkedEncoding
	}
	if c.BastionMode != nil {
		out.BastionMode = *c.BastionMode
	}
	if c.ProxyAddress != nil {
		out.ProxyAddress = *c.ProxyAddress
	}
	if c.ProxyPort != nil {
		out.ProxyPort = *c.ProxyPort
	}
	if c.ProxyType != nil {
		out.ProxyType = *c.ProxyType
	}
	if len(c.IPRules) > 0 {
		for _, r := range c.IPRules {
			rule, err := ipaccess.NewRuleByCIDR(r.Prefix, r.Ports, r.Allow)
			if err == nil {
				out.IPRules = append(out.IPRules, rule)
			}
		}
	}
	if c.Http2Origin != nil {
		out.Http2Origin = *c.Http2Origin
	}
	return out
}

// OriginRequestConfig configures how Cloudflared sends requests to origin
// services.
// Note: To specify a time.Duration in go-yaml, use e.g. "3s" or "24h".
type OriginRequestConfig struct {
	// HTTP proxy timeout for establishing a new connection
	ConnectTimeout config.CustomDuration `yaml:"connectTimeout" json:"connectTimeout"`
	// HTTP proxy timeout for completing a TLS handshake
	TLSTimeout config.CustomDuration `yaml:"tlsTimeout" json:"tlsTimeout"`
	// HTTP proxy TCP keepalive duration
	TCPKeepAlive config.CustomDuration `yaml:"tcpKeepAlive" json:"tcpKeepAlive"`
	// HTTP proxy should disable "happy eyeballs" for IPv4/v6 fallback
	NoHappyEyeballs bool `yaml:"noHappyEyeballs" json:"noHappyEyeballs"`
	// HTTP proxy timeout for closing an idle connection
	KeepAliveTimeout config.CustomDuration `yaml:"keepAliveTimeout" json:"keepAliveTimeout"`
	// HTTP proxy maximum keepalive connection pool size
	KeepAliveConnections int `yaml:"keepAliveConnections" json:"keepAliveConnections"`
	// Sets the HTTP Host header for the local webserver.
	HTTPHostHeader string `yaml:"httpHostHeader" json:"httpHostHeader"`
	// Hostname on the origin server certificate.
	OriginServerName string `yaml:"originServerName" json:"originServerName"`
	// Path to the CA for the certificate of your origin.
	// This option should be used only if your certificate is not signed by Cloudflare.
	CAPool string `yaml:"caPool" json:"caPool"`
	// Disables TLS verification of the certificate presented by your origin.
	// Will allow any certificate from the origin to be accepted.
	// Note: The connection from your machine to Cloudflare's Edge is still encrypted.
	NoTLSVerify bool `yaml:"noTLSVerify" json:"noTLSVerify"`
	// Disables chunked transfer encoding.
	// Useful if you are running a WSGI server.
	DisableChunkedEncoding bool `yaml:"disableChunkedEncoding" json:"disableChunkedEncoding"`
	// Runs as jump host
	BastionMode bool `yaml:"bastionMode" json:"bastionMode"`
	// Listen address for the proxy.
	ProxyAddress string `yaml:"proxyAddress" json:"proxyAddress"`
	// Listen port for the proxy.
	ProxyPort uint `yaml:"proxyPort" json:"proxyPort"`
	// What sort of proxy should be started
	ProxyType string `yaml:"proxyType" json:"proxyType"`
	// IP rules for the proxy service
	IPRules []ipaccess.Rule `yaml:"ipRules" json:"ipRules"`
	// Attempt to connect to origin with HTTP/2
	Http2Origin bool `yaml:"http2Origin" json:"http2Origin"`
}

func (defaults *OriginRequestConfig) setConnectTimeout(overrides config.OriginRequestConfig) {
	if val := overrides.ConnectTimeout; val != nil {
		defaults.ConnectTimeout = *val
	}
}

func (defaults *OriginRequestConfig) setTLSTimeout(overrides config.OriginRequestConfig) {
	if val := overrides.TLSTimeout; val != nil {
		defaults.TLSTimeout = *val
	}
}

func (defaults *OriginRequestConfig) setNoHappyEyeballs(overrides config.OriginRequestConfig) {
	if val := overrides.NoHappyEyeballs; val != nil {
		defaults.NoHappyEyeballs = *val
	}
}

func (defaults *OriginRequestConfig) setKeepAliveConnections(overrides config.OriginRequestConfig) {
	if val := overrides.KeepAliveConnections; val != nil {
		defaults.KeepAliveConnections = *val
	}
}

func (defaults *OriginRequestConfig) setKeepAliveTimeout(overrides config.OriginRequestConfig) {
	if val := overrides.KeepAliveTimeout; val != nil {
		defaults.KeepAliveTimeout = *val
	}
}

func (defaults *OriginRequestConfig) setTCPKeepAlive(overrides config.OriginRequestConfig) {
	if val := overrides.TCPKeepAlive; val != nil {
		defaults.TCPKeepAlive = *val
	}
}

func (defaults *OriginRequestConfig) setHTTPHostHeader(overrides config.OriginRequestConfig) {
	if val := overrides.HTTPHostHeader; val != nil {
		defaults.HTTPHostHeader = *val
	}
}

func (defaults *OriginRequestConfig) setOriginServerName(overrides config.OriginRequestConfig) {
	if val := overrides.OriginServerName; val != nil {
		defaults.OriginServerName = *val
	}
}

func (defaults *OriginRequestConfig) setCAPool(overrides config.OriginRequestConfig) {
	if val := overrides.CAPool; val != nil {
		defaults.CAPool = *val
	}
}

func (defaults *OriginRequestConfig) setNoTLSVerify(overrides config.OriginRequestConfig) {
	if val := overrides.NoTLSVerify; val != nil {
		defaults.NoTLSVerify = *val
	}
}

func (defaults *OriginRequestConfig) setDisableChunkedEncoding(overrides config.OriginRequestConfig) {
	if val := overrides.DisableChunkedEncoding; val != nil {
		defaults.DisableChunkedEncoding = *val
	}
}

func (defaults *OriginRequestConfig) setBastionMode(overrides config.OriginRequestConfig) {
	if val := overrides.BastionMode; val != nil {
		defaults.BastionMode = *val
	}
}

func (defaults *OriginRequestConfig) setProxyPort(overrides config.OriginRequestConfig) {
	if val := overrides.ProxyPort; val != nil {
		defaults.ProxyPort = *val
	}
}

func (defaults *OriginRequestConfig) setProxyAddress(overrides config.OriginRequestConfig) {
	if val := overrides.ProxyAddress; val != nil {
		defaults.ProxyAddress = *val
	}
}

func (defaults *OriginRequestConfig) setProxyType(overrides config.OriginRequestConfig) {
	if val := overrides.ProxyType; val != nil {
		defaults.ProxyType = *val
	}
}

func (defaults *OriginRequestConfig) setIPRules(overrides config.OriginRequestConfig) {
	if val := overrides.IPRules; len(val) > 0 {
		ipAccessRule := make([]ipaccess.Rule, len(overrides.IPRules))
		for i, r := range overrides.IPRules {
			rule, err := ipaccess.NewRuleByCIDR(r.Prefix, r.Ports, r.Allow)
			if err == nil {
				ipAccessRule[i] = rule
			}
		}
		defaults.IPRules = ipAccessRule
	}
}

func (defaults *OriginRequestConfig) setHttp2Origin(overrides config.OriginRequestConfig) {
	if val := overrides.Http2Origin; val != nil {
		defaults.Http2Origin = *val
	}
}

// SetConfig gets config for the requests that cloudflared sends to origins.
// Each field has a setter method which sets a value for the field by trying to find:
//   1. The user config for this rule
//   2. The user config for the overall ingress config
//   3. Defaults chosen by the cloudflared team
//   4. Golang zero values for that type
// If an earlier option isn't set, it will try the next option down.
func setConfig(defaults OriginRequestConfig, overrides config.OriginRequestConfig) OriginRequestConfig {
	cfg := defaults
	cfg.setConnectTimeout(overrides)
	cfg.setTLSTimeout(overrides)
	cfg.setNoHappyEyeballs(overrides)
	cfg.setKeepAliveConnections(overrides)
	cfg.setKeepAliveTimeout(overrides)
	cfg.setTCPKeepAlive(overrides)
	cfg.setHTTPHostHeader(overrides)
	cfg.setOriginServerName(overrides)
	cfg.setCAPool(overrides)
	cfg.setNoTLSVerify(overrides)
	cfg.setDisableChunkedEncoding(overrides)
	cfg.setBastionMode(overrides)
	cfg.setProxyPort(overrides)
	cfg.setProxyAddress(overrides)
	cfg.setProxyType(overrides)
	cfg.setIPRules(overrides)
	cfg.setHttp2Origin(overrides)
	return cfg
}

func ConvertToRawOriginConfig(c OriginRequestConfig) config.OriginRequestConfig {
	var connectTimeout *config.CustomDuration
	var tlsTimeout *config.CustomDuration
	var tcpKeepAlive *config.CustomDuration
	var keepAliveConnections *int
	var keepAliveTimeout *config.CustomDuration
	var proxyAddress *string

	if c.ConnectTimeout != defaultHTTPConnectTimeout {
		connectTimeout = &c.ConnectTimeout
	}
	if c.TLSTimeout != defaultTLSTimeout {
		tlsTimeout = &c.TLSTimeout
	}
	if c.TCPKeepAlive != defaultTCPKeepAlive {
		tcpKeepAlive = &c.TCPKeepAlive
	}
	if c.KeepAliveConnections != defaultKeepAliveConnections {
		keepAliveConnections = &c.KeepAliveConnections
	}
	if c.KeepAliveTimeout != defaultKeepAliveTimeout {
		keepAliveTimeout = &c.KeepAliveTimeout
	}
	if c.ProxyAddress != defaultProxyAddress {
		proxyAddress = &c.ProxyAddress
	}

	return config.OriginRequestConfig{
		ConnectTimeout:         connectTimeout,
		TLSTimeout:             tlsTimeout,
		TCPKeepAlive:           tcpKeepAlive,
		NoHappyEyeballs:        defaultBoolToNil(c.NoHappyEyeballs),
		KeepAliveConnections:   keepAliveConnections,
		KeepAliveTimeout:       keepAliveTimeout,
		HTTPHostHeader:         emptyStringToNil(c.HTTPHostHeader),
		OriginServerName:       emptyStringToNil(c.OriginServerName),
		CAPool:                 emptyStringToNil(c.CAPool),
		NoTLSVerify:            defaultBoolToNil(c.NoTLSVerify),
		DisableChunkedEncoding: defaultBoolToNil(c.DisableChunkedEncoding),
		BastionMode:            defaultBoolToNil(c.BastionMode),
		ProxyAddress:           proxyAddress,
		ProxyPort:              zeroUIntToNil(c.ProxyPort),
		ProxyType:              emptyStringToNil(c.ProxyType),
		IPRules:                convertToRawIPRules(c.IPRules),
		Http2Origin:            defaultBoolToNil(c.Http2Origin),
	}
}

func convertToRawIPRules(ipRules []ipaccess.Rule) []config.IngressIPRule {
	result := make([]config.IngressIPRule, 0)
	for _, r := range ipRules {
		cidr := r.StringCIDR()

		newRule := config.IngressIPRule{
			Prefix: &cidr,
			Ports:  r.Ports(),
			Allow:  r.RulePolicy(),
		}

		result = append(result, newRule)
	}

	return result
}

func defaultBoolToNil(b bool) *bool {
	if b == false {
		return nil
	}

	return &b
}

func emptyStringToNil(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func zeroUIntToNil(v uint) *uint {
	if v == 0 {
		return nil
	}

	return &v
}
