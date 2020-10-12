package ingress

import (
	"net/url"
	"reflect"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_parseIngress(t *testing.T) {
	localhost8000, err := url.Parse("https://localhost:8000")
	require.NoError(t, err)
	localhost8001, err := url.Parse("https://localhost:8001")
	require.NoError(t, err)
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
					Service:  localhost8000,
				},
				{
					Hostname: "*",
					Service:  localhost8001,
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
					Service:  localhost8000,
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
					Service: localhost8000,
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
			name: "Invalid YAML",
			args: args{rawYAML: `
key: "value
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
			name: "Can't use --url",
			args: args{rawYAML: `
url: localhost:8080
ingress:
  - hostname: "*.example.com"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIngress([]byte(tt.args.rawYAML))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIngress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseIngress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func MustParse(t *testing.T, rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u
}

func Test_rule_matches(t *testing.T) {
	type fields struct {
		Hostname string
		Path     *regexp.Regexp
		Service  *url.URL
	}
	type args struct {
		requestURL *url.URL
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "Just hostname, pass",
			fields: fields{
				Hostname: "example.com",
			},
			args: args{
				requestURL: MustParse(t, "https://example.com"),
			},
			want: true,
		},
		{
			name: "Entire hostname is wildcard, should match everything",
			fields: fields{
				Hostname: "*",
			},
			args: args{
				requestURL: MustParse(t, "https://example.com"),
			},
			want: true,
		},
		{
			name: "Just hostname, fail",
			fields: fields{
				Hostname: "example.com",
			},
			args: args{
				requestURL: MustParse(t, "https://foo.bar"),
			},
			want: false,
		},
		{
			name: "Just wildcard hostname, pass",
			fields: fields{
				Hostname: "*.example.com",
			},
			args: args{
				requestURL: MustParse(t, "https://adam.example.com"),
			},
			want: true,
		},
		{
			name: "Just wildcard hostname, fail",
			fields: fields{
				Hostname: "*.example.com",
			},
			args: args{
				requestURL: MustParse(t, "https://tunnel.com"),
			},
			want: false,
		},
		{
			name: "Just wildcard outside of subdomain in hostname, fail",
			fields: fields{
				Hostname: "*example.com",
			},
			args: args{
				requestURL: MustParse(t, "https://www.example.com"),
			},
			want: false,
		},
		{
			name: "Wildcard over multiple subdomains",
			fields: fields{
				Hostname: "*.example.com",
			},
			args: args{
				requestURL: MustParse(t, "https://adam.chalmers.example.com"),
			},
			want: true,
		},
		{
			name: "Hostname and path",
			fields: fields{
				Hostname: "*.example.com",
				Path:     regexp.MustCompile("/static/.*\\.html"),
			},
			args: args{
				requestURL: MustParse(t, "https://www.example.com/static/index.html"),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Rule{
				Hostname: tt.fields.Hostname,
				Path:     tt.fields.Path,
				Service:  tt.fields.Service,
			}
			u := tt.args.requestURL
			if got := r.Matches(u.Hostname(), u.Path); got != tt.want {
				t.Errorf("rule.matches() = %v, want %v", got, tt.want)
			}
		})
	}
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
	rules, err := ParseIngress([]byte(rulesYAML))
	if err != nil {
		b.Error(err)
	}
	for n := 0; n < b.N; n++ {
		FindMatchingRule("tunnel1.example.com", "", rules)
		FindMatchingRule("tunnel2.example.com", "", rules)
		FindMatchingRule("tunnel3.example.com", "", rules)
	}
}
