package ingress

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
)

func TestParseUnixSocket(t *testing.T) {
	rawYAML := `
ingress:
- service: unix:/tmp/echo.sock
`
	ing, err := ParseIngress(MustReadIngress(rawYAML))
	require.NoError(t, err)
	_, ok := ing.Rules[0].Service.(*unixSocketPath)
	require.True(t, ok)
}

func Test_parseIngress(t *testing.T) {
	localhost8000 := MustParseURL(t, "https://localhost:8000")
	localhost8001 := MustParseURL(t, "https://localhost:8001")
	defaultConfig := setConfig(originRequestFromYAML(config.OriginRequestConfig{}), config.OriginRequestConfig{})
	require.Equal(t, defaultKeepAliveConnections, defaultConfig.KeepAliveConnections)
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
					Service:  &localService{URL: localhost8000},
					Config:   defaultConfig,
				},
				{
					Hostname: "*",
					Service:  &localService{URL: localhost8001},
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
					Service:  &localService{URL: localhost8000},
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
					Service: &localService{URL: localhost8000},
					Config:  defaultConfig,
				},
			},
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

func MustParseURL(t *testing.T, rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u
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

func MustReadIngress(s string) *config.Configuration {
	var conf config.Configuration
	err := yaml.Unmarshal([]byte(s), &conf)
	if err != nil {
		panic(err)
	}
	return &conf
}
