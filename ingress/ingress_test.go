package ingress

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	yaml "gopkg.in/yaml.v3"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ipaccess"
	"github.com/cloudflare/cloudflared/tlsconfig"
)

func TestParseUnixSocket(t *testing.T) {
	rawYAML := `
ingress:
- service: unix:/tmp/echo.sock
`
	ing, err := ParseIngress(MustReadIngress(rawYAML))
	require.NoError(t, err)
	s, ok := ing.Rules[0].Service.(*unixSocketPath)
	require.True(t, ok)
	require.Equal(t, "http", s.scheme)
}

func TestParseUnixSocketTLS(t *testing.T) {
	rawYAML := `
ingress:
- service: unix+tls:/tmp/echo.sock
`
	ing, err := ParseIngress(MustReadIngress(rawYAML))
	require.NoError(t, err)
	s, ok := ing.Rules[0].Service.(*unixSocketPath)
	require.True(t, ok)
	require.Equal(t, "https", s.scheme)
}

func TestParseIngressNilConfig(t *testing.T) {
	_, err := ParseIngress(nil)
	require.Error(t, err)
}

func TestParseIngress(t *testing.T) {
	localhost8000 := MustParseURL(t, "https://localhost:8000")
	localhost8001 := MustParseURL(t, "https://localhost:8001")
	fourOhFour := newStatusCode(404)
	defaultConfig := setConfig(originRequestFromConfig(config.OriginRequestConfig{}), config.OriginRequestConfig{})
	require.Equal(t, defaultKeepAliveConnections, defaultConfig.KeepAliveConnections)
	tr := true
	type args struct {
		rawYAML string
	}
	tests := []struct {
		name    string
		args    args
		want    []Rule
		wantErr bool
	}{
		{
			name:    "Empty file",
			args:    args{rawYAML: ""},
			wantErr: true,
		},
		{
			name: "Multiple rules",
			args: args{rawYAML: `
ingress:
 - hostname: tunnel1.example.com
   service: https://localhost:8000
 - hostname: "*"
   service: https://localhost:8001
`},
			want: []Rule{
				{
					Hostname: "tunnel1.example.com",
					Service:  &httpService{url: localhost8000},
					Config:   defaultConfig,
				},
				{
					Hostname: "*",
					Service:  &httpService{url: localhost8001},
					Config:   defaultConfig,
				},
			},
		},
		{
			name: "Extra keys",
			args: args{rawYAML: `
ingress:
 - hostname: "*"
   service: https://localhost:8000
extraKey: extraValue
`},
			want: []Rule{
				{
					Hostname: "*",
					Service:  &httpService{url: localhost8000},
					Config:   defaultConfig,
				},
			},
		},
		{
			name: "ws service",
			args: args{rawYAML: `
ingress:
 - hostname: "*"
   service: wss://localhost:8000
`},
			want: []Rule{
				{
					Hostname: "*",
					Service:  &httpService{url: MustParseURL(t, "wss://localhost:8000")},
					Config:   defaultConfig,
				},
			},
		},
		{
			name: "Hostname can be omitted",
			args: args{rawYAML: `
ingress:
 - service: https://localhost:8000
`},
			want: []Rule{
				{
					Service: &httpService{url: localhost8000},
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "Unicode domain",
			args: args{rawYAML: `
ingress:
 - hostname: môô.cloudflare.com
   service: https://localhost:8000
 - service: https://localhost:8001
`},
			want: []Rule{
				{
					Hostname:         "môô.cloudflare.com",
					punycodeHostname: "xn--m-xgaa.cloudflare.com",
					Service:          &httpService{url: localhost8000},
					Config:           defaultConfig,
				},
				{
					Service: &httpService{url: localhost8001},
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "Invalid unicode domain",
			args: args{rawYAML: fmt.Sprintf(`
ingress:
 - hostname: %s
   service: https://localhost:8000
`, string(rune(0xd8f3))+".cloudflare.com")},
			wantErr: true,
		},
		{
			name: "Invalid service",
			args: args{rawYAML: `
ingress:
 - hostname: "*"
   service: https://local host:8000
`},
			wantErr: true,
		},
		{
			name: "Last rule isn't catchall",
			args: args{rawYAML: `
ingress:
 - hostname: example.com
   service: https://localhost:8000
`},
			wantErr: true,
		},
		{
			name: "First rule is catchall",
			args: args{rawYAML: `
ingress:
 - service: https://localhost:8000
 - hostname: example.com
   service: https://localhost:8000
`},
			wantErr: true,
		},
		{
			name: "Catch-all rule can't have a path",
			args: args{rawYAML: `
ingress:
 - service: https://localhost:8001
   path: /subpath1/(.*)/subpath2
`},
			wantErr: true,
		},
		{
			name: "Invalid regex",
			args: args{rawYAML: `
ingress:
 - hostname: example.com
   service: https://localhost:8000
   path: "*/subpath2"
 - service: https://localhost:8001
`},
			wantErr: true,
		},
		{
			name: "Service must have a scheme",
			args: args{rawYAML: `
ingress:
 - service: localhost:8000
`},
			wantErr: true,
		},
		{
			name: "Wildcard not at start",
			args: args{rawYAML: `
ingress:
 - hostname: "test.*.example.com"
   service: https://localhost:8000
`},
			wantErr: true,
		},
		{
			name: "Service can't have a path",
			args: args{rawYAML: `
ingress:
 - service: https://localhost:8000/static/
`},
			wantErr: true,
		},
		{
			name: "Invalid HTTP status",
			args: args{rawYAML: `
ingress:
 - service: http_status:asdf
`},
			wantErr: true,
		},
		{
			name: "Invalid HTTP status code",
			args: args{rawYAML: `
ingress:
 - service: http_status:8080
`},
			wantErr: true,
		},
		{
			name: "Valid HTTP status",
			args: args{rawYAML: `
ingress:
 - service: http_status:404
`},
			want: []Rule{
				{
					Hostname: "",
					Service:  &fourOhFour,
					Config:   defaultConfig,
				},
			},
		},
		{
			name: "Valid hello world service",
			args: args{rawYAML: `
ingress:
 - service: hello_world
`},
			want: []Rule{
				{
					Hostname: "",
					Service:  new(helloWorld),
					Config:   defaultConfig,
				},
			},
		},
		{
			name: "TCP services",
			args: args{rawYAML: `
ingress:
- hostname: tcp.foo.com
  service: tcp://127.0.0.1
- hostname: tcp2.foo.com
  service: tcp://localhost:8000
- service: http_status:404
`},
			want: []Rule{
				{
					Hostname: "tcp.foo.com",
					Service:  newTCPOverWSService(MustParseURL(t, "tcp://127.0.0.1:7864")),
					Config:   defaultConfig,
				},
				{
					Hostname: "tcp2.foo.com",
					Service:  newTCPOverWSService(MustParseURL(t, "tcp://localhost:8000")),
					Config:   defaultConfig,
				},
				{
					Service: &fourOhFour,
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "SSH services",
			args: args{rawYAML: `
ingress:
- service: ssh://127.0.0.1
`},
			want: []Rule{
				{
					Service: newTCPOverWSService(MustParseURL(t, "ssh://127.0.0.1:22")),
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "RDP services",
			args: args{rawYAML: `
ingress:
- service: rdp://127.0.0.1
`},
			want: []Rule{
				{
					Service: newTCPOverWSService(MustParseURL(t, "rdp://127.0.0.1:3389")),
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "SMB services",
			args: args{rawYAML: `
ingress:
- service: smb://127.0.0.1
`},
			want: []Rule{
				{
					Service: newTCPOverWSService(MustParseURL(t, "smb://127.0.0.1:445")),
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "Other TCP services",
			args: args{rawYAML: `
ingress:
- service: ftp://127.0.0.1
`},
			want: []Rule{
				{
					Service: newTCPOverWSService(MustParseURL(t, "ftp://127.0.0.1")),
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "SOCKS services",
			args: args{rawYAML: `
ingress:
- hostname: socks.foo.com
  service: socks-proxy
  originRequest:
    ipRules:
      - prefix: 1.1.1.0/24
        ports: [80, 443]
        allow: true
      - prefix: 0.0.0.0/0
        allow: false
- service: http_status:404
`},
			want: []Rule{
				{
					Hostname: "socks.foo.com",
					Service:  newSocksProxyOverWSService(accessPolicy()),
					Config: setConfig(originRequestFromConfig(config.OriginRequestConfig{}), config.OriginRequestConfig{IPRules: []config.IngressIPRule{
						{
							Prefix: ipRulePrefix("1.1.1.0/24"),
							Ports:  []int{80, 443},
							Allow:  true,
						},
						{
							Prefix: ipRulePrefix("0.0.0.0/0"),
							Allow:  false,
						},
					}}),
				},
				{
					Service: &fourOhFour,
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "URL isn't necessary if using bastion",
			args: args{rawYAML: `
ingress:
- hostname: bastion.foo.com
  originRequest:
    bastionMode: true
- service: http_status:404
`},
			want: []Rule{
				{
					Hostname: "bastion.foo.com",
					Service:  newBastionService(),
					Config:   setConfig(originRequestFromConfig(config.OriginRequestConfig{}), config.OriginRequestConfig{BastionMode: &tr}),
				},
				{
					Service: &fourOhFour,
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "Bastion service",
			args: args{rawYAML: `
ingress:
- hostname: bastion.foo.com
  service: bastion
- service: http_status:404
`},
			want: []Rule{
				{
					Hostname: "bastion.foo.com",
					Service:  newBastionService(),
					Config:   setConfig(originRequestFromConfig(config.OriginRequestConfig{}), config.OriginRequestConfig{BastionMode: &tr}),
				},
				{
					Service: &fourOhFour,
					Config:  defaultConfig,
				},
			},
		},
		{
			name: "Hostname contains port",
			args: args{rawYAML: `
ingress:
 - hostname: "test.example.com:443"
   service: https://localhost:8000
 - hostname: "*"
   service: https://localhost:8001
`},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIngress(MustReadIngress(tt.args.rawYAML))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIngress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			require.Equal(t, tt.want, got.Rules)
		})
	}
}

func ipRulePrefix(s string) *string {
	return &s
}

func TestSingleOriginSetsConfig(t *testing.T) {
	flagSet := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	flagSet.Bool("hello-world", true, "")
	flagSet.Duration(ProxyConnectTimeoutFlag, time.Second, "")
	flagSet.Duration(ProxyTLSTimeoutFlag, time.Second, "")
	flagSet.Duration(ProxyTCPKeepAliveFlag, time.Second, "")
	flagSet.Bool(ProxyNoHappyEyeballsFlag, true, "")
	flagSet.Int(ProxyKeepAliveConnectionsFlag, 10, "")
	flagSet.Duration(ProxyKeepAliveTimeoutFlag, time.Second, "")
	flagSet.String(HTTPHostHeaderFlag, "example.com:8080", "")
	flagSet.String(OriginServerNameFlag, "example.com", "")
	flagSet.String(tlsconfig.OriginCAPoolFlag, "/etc/certs/ca.pem", "")
	flagSet.Bool(NoTLSVerifyFlag, true, "")
	flagSet.Bool(NoChunkedEncodingFlag, true, "")
	flagSet.Bool(config.BastionFlag, true, "")
	flagSet.String(ProxyAddressFlag, "localhost:8080", "")
	flagSet.Uint(ProxyPortFlag, 8080, "")
	flagSet.Bool(Socks5Flag, true, "")

	cliCtx := cli.NewContext(cli.NewApp(), flagSet, nil)
	err := cliCtx.Set("hello-world", "true")
	require.NoError(t, err)
	err = cliCtx.Set(ProxyConnectTimeoutFlag, "1s")
	require.NoError(t, err)
	err = cliCtx.Set(ProxyTLSTimeoutFlag, "1s")
	require.NoError(t, err)
	err = cliCtx.Set(ProxyTCPKeepAliveFlag, "1s")
	require.NoError(t, err)
	err = cliCtx.Set(ProxyNoHappyEyeballsFlag, "true")
	require.NoError(t, err)
	err = cliCtx.Set(ProxyKeepAliveConnectionsFlag, "10")
	require.NoError(t, err)
	err = cliCtx.Set(ProxyKeepAliveTimeoutFlag, "1s")
	require.NoError(t, err)
	err = cliCtx.Set(HTTPHostHeaderFlag, "example.com:8080")
	require.NoError(t, err)
	err = cliCtx.Set(OriginServerNameFlag, "example.com")
	require.NoError(t, err)
	err = cliCtx.Set(tlsconfig.OriginCAPoolFlag, "/etc/certs/ca.pem")
	require.NoError(t, err)
	err = cliCtx.Set(NoTLSVerifyFlag, "true")
	require.NoError(t, err)
	err = cliCtx.Set(NoChunkedEncodingFlag, "true")
	require.NoError(t, err)
	err = cliCtx.Set(config.BastionFlag, "true")
	require.NoError(t, err)
	err = cliCtx.Set(ProxyAddressFlag, "localhost:8080")
	require.NoError(t, err)
	err = cliCtx.Set(ProxyPortFlag, "8080")
	require.NoError(t, err)
	err = cliCtx.Set(Socks5Flag, "true")
	require.NoError(t, err)

	allowURLFromArgs := false
	require.NoError(t, err)
	ingress, err := parseCLIIngress(cliCtx, allowURLFromArgs)
	require.NoError(t, err)

	assert.Equal(t, config.CustomDuration{Duration: time.Second}, ingress.Rules[0].Config.ConnectTimeout)
	assert.Equal(t, config.CustomDuration{Duration: time.Second}, ingress.Rules[0].Config.TLSTimeout)
	assert.Equal(t, config.CustomDuration{Duration: time.Second}, ingress.Rules[0].Config.TCPKeepAlive)
	assert.True(t, ingress.Rules[0].Config.NoHappyEyeballs)
	assert.Equal(t, 10, ingress.Rules[0].Config.KeepAliveConnections)
	assert.Equal(t, config.CustomDuration{Duration: time.Second}, ingress.Rules[0].Config.KeepAliveTimeout)
	assert.Equal(t, "example.com:8080", ingress.Rules[0].Config.HTTPHostHeader)
	assert.Equal(t, "example.com", ingress.Rules[0].Config.OriginServerName)
	assert.Equal(t, "/etc/certs/ca.pem", ingress.Rules[0].Config.CAPool)
	assert.True(t, ingress.Rules[0].Config.NoTLSVerify)
	assert.True(t, ingress.Rules[0].Config.DisableChunkedEncoding)
	assert.True(t, ingress.Rules[0].Config.BastionMode)
	assert.Equal(t, "localhost:8080", ingress.Rules[0].Config.ProxyAddress)
	assert.Equal(t, uint(8080), ingress.Rules[0].Config.ProxyPort)
	assert.Equal(t, socksProxy, ingress.Rules[0].Config.ProxyType)
}

func TestSingleOriginServices(t *testing.T) {
	host := "://localhost:8080"
	httpURL := urlMustParse("http" + host)
	tcpURL := urlMustParse("tcp" + host)
	unix := "unix://service"
	newCli := func(params ...string) *cli.Context {
		flagSet := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		flagSet.Bool("hello-world", false, "")
		flagSet.Bool("bastion", false, "")
		flagSet.String("url", "", "")
		flagSet.String("unix-socket", "", "")
		cliCtx := cli.NewContext(cli.NewApp(), flagSet, nil)
		for i := 0; i < len(params); i += 2 {
			cliCtx.Set(params[i], params[i+1])
		}

		return cliCtx
	}

	tests := []struct {
		name            string
		cli             *cli.Context
		expectedService OriginService
		err             error
	}{
		{
			name:            "Valid hello-world",
			cli:             newCli("hello-world", "true"),
			expectedService: &helloWorld{},
		},
		{
			name:            "Valid bastion",
			cli:             newCli("bastion", "true"),
			expectedService: newBastionService(),
		},
		{
			name:            "Valid http url",
			cli:             newCli("url", httpURL.String()),
			expectedService: &httpService{url: httpURL},
		},
		{
			name:            "Valid tcp url",
			cli:             newCli("url", tcpURL.String()),
			expectedService: newTCPOverWSService(tcpURL),
		},
		{
			name:            "Valid unix-socket",
			cli:             newCli("unix-socket", unix),
			expectedService: &unixSocketPath{path: unix, scheme: "http"},
		},
		{
			name: "No origins defined",
			cli:  newCli(),
			err:  ErrNoIngressRulesCLI,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ingress, err := parseCLIIngress(test.cli, false)
			require.Equal(t, err, test.err)
			if test.err != nil {
				return
			}
			require.Equal(t, 1, len(ingress.Rules))
			rule := ingress.Rules[0]
			require.Equal(t, test.expectedService, rule.Service)
		})
	}
}

func urlMustParse(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestSingleOriginServices_URL(t *testing.T) {
	host := "://localhost:8080"
	newCli := func(param string, value string) *cli.Context {
		flagSet := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		flagSet.String("url", "", "")
		cliCtx := cli.NewContext(cli.NewApp(), flagSet, nil)
		cliCtx.Set(param, value)
		return cliCtx
	}

	httpTests := []string{"http", "https"}
	for _, test := range httpTests {
		t.Run(test, func(t *testing.T) {
			url := urlMustParse(test + host)
			ingress, err := parseCLIIngress(newCli("url", url.String()), false)
			require.NoError(t, err)
			require.Equal(t, 1, len(ingress.Rules))
			rule := ingress.Rules[0]
			require.Equal(t, &httpService{url: url}, rule.Service)
		})
	}

	tcpTests := []string{"ssh", "rdp", "smb", "tcp"}
	for _, test := range tcpTests {
		t.Run(test, func(t *testing.T) {
			url := urlMustParse(test + host)
			ingress, err := parseCLIIngress(newCli("url", url.String()), false)
			require.NoError(t, err)
			require.Equal(t, 1, len(ingress.Rules))
			rule := ingress.Rules[0]
			require.Equal(t, newTCPOverWSService(url), rule.Service)
		})
	}
}

func TestFindMatchingRule(t *testing.T) {
	ingress := Ingress{
		Rules: []Rule{
			{
				Hostname: "tunnel-a.example.com",
				Path:     nil,
			},
			{
				Hostname: "tunnel-b.example.com",
				Path:     MustParsePath(t, "/health"),
			},
			{
				Hostname: "*",
			},
		},
	}

	tests := []struct {
		host          string
		path          string
		req           *http.Request
		wantRuleIndex int
	}{
		{
			host:          "tunnel-a.example.com",
			path:          "/",
			wantRuleIndex: 0,
		},
		{
			host:          "tunnel-a.example.com",
			path:          "/pages/about",
			wantRuleIndex: 0,
		},
		{
			host:          "tunnel-a.example.com:443",
			path:          "/pages/about",
			wantRuleIndex: 0,
		},
		{
			host:          "tunnel-b.example.com",
			path:          "/health",
			wantRuleIndex: 1,
		},
		{
			host:          "tunnel-b.example.com",
			path:          "/index.html",
			wantRuleIndex: 2,
		},
		{
			host:          "tunnel-c.example.com",
			path:          "/",
			wantRuleIndex: 2,
		},
	}

	for _, test := range tests {
		_, ruleIndex := ingress.FindMatchingRule(test.host, test.path)
		assert.Equal(t, test.wantRuleIndex, ruleIndex, fmt.Sprintf("Expect host=%s, path=%s to match rule %d, got %d", test.host, test.path, test.wantRuleIndex, ruleIndex))
	}
}

func TestIsHTTPService(t *testing.T) {
	tests := []struct {
		url    *url.URL
		isHTTP bool
	}{
		{
			url:    MustParseURL(t, "http://localhost"),
			isHTTP: true,
		},
		{
			url:    MustParseURL(t, "https://127.0.0.1:8000"),
			isHTTP: true,
		},
		{
			url:    MustParseURL(t, "ws://localhost"),
			isHTTP: true,
		},
		{
			url:    MustParseURL(t, "wss://localhost:8000"),
			isHTTP: true,
		},
		{
			url:    MustParseURL(t, "tcp://localhost:9000"),
			isHTTP: false,
		},
	}
	for _, test := range tests {
		assert.Equal(t, test.isHTTP, isHTTPService(test.url))
	}
}

func MustParsePath(t *testing.T, path string) *Regexp {
	regexp, err := regexp.Compile(path)
	assert.NoError(t, err)
	return &Regexp{Regexp: regexp}
}

func MustParseURL(t *testing.T, rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u
}

func accessPolicy() *ipaccess.Policy {
	cidr1 := "1.1.1.0/24"
	cidr2 := "0.0.0.0/0"
	rule1, _ := ipaccess.NewRuleByCIDR(&cidr1, []int{80, 443}, true)
	rule2, _ := ipaccess.NewRuleByCIDR(&cidr2, nil, false)
	rules := []ipaccess.Rule{rule1, rule2}
	accessPolicy, _ := ipaccess.NewPolicy(false, rules)
	return accessPolicy
}

func BenchmarkFindMatch(b *testing.B) {
	rulesYAML := `
ingress:
 - hostname: tunnel1.example.com
   service: https://localhost:8000
 - hostname: tunnel2.example.com
   service: https://localhost:8001
 - hostname: "*"
   service: https://localhost:8002
`

	ing, err := ParseIngress(MustReadIngress(rulesYAML))
	if err != nil {
		b.Error(err)
	}

	for n := 0; n < b.N; n++ {
		ing.FindMatchingRule("tunnel1.example.com", "")
		ing.FindMatchingRule("tunnel2.example.com", "")
		ing.FindMatchingRule("tunnel3.example.com", "")
	}
}

func TestParseAccessConfig(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.AccessConfig
		expectError bool
	}{
		{
			name:        "Config required with teamName only",
			cfg:         config.AccessConfig{Required: true, TeamName: "team"},
			expectError: false,
		},
		{
			name:        "required false",
			cfg:         config.AccessConfig{Required: false},
			expectError: false,
		},
		{
			name:        "required true but empty config",
			cfg:         config.AccessConfig{Required: true},
			expectError: false,
		},
		{
			name:        "complete config",
			cfg:         config.AccessConfig{Required: true, TeamName: "team", AudTag: []string{"a"}},
			expectError: false,
		},
		{
			name:        "required true with audTags but no teamName",
			cfg:         config.AccessConfig{Required: true, AudTag: []string{"a"}},
			expectError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAccessConfiguration(&test.cfg)
			require.Equal(t, err != nil, test.expectError)
		})
	}
}

func MustReadIngress(s string) *config.Configuration {
	var conf config.Configuration
	err := yaml.Unmarshal([]byte(s), &conf)
	if err != nil {
		panic(err)
	}
	return &conf
}
