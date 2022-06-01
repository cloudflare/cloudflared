package ingress

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/config"
)

func Test_rule_matches(t *testing.T) {
	type fields struct {
		Hostname string
		Path     *Regexp
		Service  OriginService
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
				requestURL: MustParseURL(t, "https://example.com"),
			},
			want: true,
		},
		{
			name: "Entire hostname is wildcard, should match everything",
			fields: fields{
				Hostname: "*",
			},
			args: args{
				requestURL: MustParseURL(t, "https://example.com"),
			},
			want: true,
		},
		{
			name: "Just hostname, fail",
			fields: fields{
				Hostname: "example.com",
			},
			args: args{
				requestURL: MustParseURL(t, "https://foo.bar"),
			},
			want: false,
		},
		{
			name: "Just wildcard hostname, pass",
			fields: fields{
				Hostname: "*.example.com",
			},
			args: args{
				requestURL: MustParseURL(t, "https://adam.example.com"),
			},
			want: true,
		},
		{
			name: "Just wildcard hostname, fail",
			fields: fields{
				Hostname: "*.example.com",
			},
			args: args{
				requestURL: MustParseURL(t, "https://tunnel.com"),
			},
			want: false,
		},
		{
			name: "Just wildcard outside of subdomain in hostname, fail",
			fields: fields{
				Hostname: "*example.com",
			},
			args: args{
				requestURL: MustParseURL(t, "https://www.example.com"),
			},
			want: false,
		},
		{
			name: "Wildcard over multiple subdomains",
			fields: fields{
				Hostname: "*.example.com",
			},
			args: args{
				requestURL: MustParseURL(t, "https://adam.chalmers.example.com"),
			},
			want: true,
		},
		{
			name: "Hostname and path",
			fields: fields{
				Hostname: "*.example.com",
				Path:     &Regexp{Regexp: regexp.MustCompile("/static/.*\\.html")},
			},
			args: args{
				requestURL: MustParseURL(t, "https://www.example.com/static/index.html"),
			},
			want: true,
		},
		{
			name: "Hostname and empty Regex",
			fields: fields{
				Hostname: "example.com",
				Path:     &Regexp{},
			},
			args: args{
				requestURL: MustParseURL(t, "https://example.com/"),
			},
			want: true,
		},
		{
			name: "Hostname and nil path",
			fields: fields{
				Hostname: "example.com",
				Path:     nil,
			},
			args: args{
				requestURL: MustParseURL(t, "https://example.com/"),
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

func TestStaticHTTPStatus(t *testing.T) {
	o := newStatusCode(404)
	buf := make([]byte, 100)

	sendReq := func() {
		resp, err := o.RoundTrip(nil)
		require.NoError(t, err)
		_, err = resp.Body.Read(buf)
		require.Equal(t, io.EOF, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, 404, resp.StatusCode)

		resp, err = o.RoundTrip(nil)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		n, err := io.Copy(w, resp.Body)
		require.NoError(t, err)
		require.Equal(t, int64(0), n)
	}
	sendReq()
	sendReq()
}

func TestMarshalJSON(t *testing.T) {
	localhost8000 := MustParseURL(t, "https://localhost:8000")
	defaultConfig := setConfig(originRequestFromConfig(config.OriginRequestConfig{}), config.OriginRequestConfig{})
	tests := []struct {
		name     string
		path     *Regexp
		expected string
		want     bool
	}{
		{
			name:     "Nil",
			path:     nil,
			expected: `{"hostname":"example.com","path":null,"service":"https://localhost:8000","originRequest":{"connectTimeout":30,"tlsTimeout":10,"tcpKeepAlive":30,"noHappyEyeballs":false,"keepAliveTimeout":90,"keepAliveConnections":100,"httpHostHeader":"","originServerName":"","caPool":"","noTLSVerify":false,"disableChunkedEncoding":false,"bastionMode":false,"proxyAddress":"127.0.0.1","proxyPort":0,"proxyType":"","ipRules":null,"http2Origin":false}}`,
			want:     true,
		},
		{
			name:     "Nil regex",
			path:     &Regexp{Regexp: nil},
			expected: `{"hostname":"example.com","path":null,"service":"https://localhost:8000","originRequest":{"connectTimeout":30,"tlsTimeout":10,"tcpKeepAlive":30,"noHappyEyeballs":false,"keepAliveTimeout":90,"keepAliveConnections":100,"httpHostHeader":"","originServerName":"","caPool":"","noTLSVerify":false,"disableChunkedEncoding":false,"bastionMode":false,"proxyAddress":"127.0.0.1","proxyPort":0,"proxyType":"","ipRules":null,"http2Origin":false}}`,
			want:     true,
		},
		{
			name:     "Empty",
			path:     &Regexp{Regexp: regexp.MustCompile("")},
			expected: `{"hostname":"example.com","path":"","service":"https://localhost:8000","originRequest":{"connectTimeout":30,"tlsTimeout":10,"tcpKeepAlive":30,"noHappyEyeballs":false,"keepAliveTimeout":90,"keepAliveConnections":100,"httpHostHeader":"","originServerName":"","caPool":"","noTLSVerify":false,"disableChunkedEncoding":false,"bastionMode":false,"proxyAddress":"127.0.0.1","proxyPort":0,"proxyType":"","ipRules":null,"http2Origin":false}}`,
			want:     true,
		},
		{
			name:     "Basic",
			path:     &Regexp{Regexp: regexp.MustCompile("/echo")},
			expected: `{"hostname":"example.com","path":"/echo","service":"https://localhost:8000","originRequest":{"connectTimeout":30,"tlsTimeout":10,"tcpKeepAlive":30,"noHappyEyeballs":false,"keepAliveTimeout":90,"keepAliveConnections":100,"httpHostHeader":"","originServerName":"","caPool":"","noTLSVerify":false,"disableChunkedEncoding":false,"bastionMode":false,"proxyAddress":"127.0.0.1","proxyPort":0,"proxyType":"","ipRules":null,"http2Origin":false}}`,
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Rule{
				Hostname: "example.com",
				Service:  &httpService{url: localhost8000},
				Path:     tt.path,
				Config:   defaultConfig,
			}
			bytes, err := json.Marshal(r)
			require.NoError(t, err)
			require.Equal(t, tt.expected, string(bytes))
		})
	}
}
