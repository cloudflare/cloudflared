package ingress

import (
	"io"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_rule_matches(t *testing.T) {
	type fields struct {
		Hostname string
		Path     *regexp.Regexp
		Service  originService
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
				Path:     regexp.MustCompile("/static/.*\\.html"),
			},
			args: args{
				requestURL: MustParseURL(t, "https://www.example.com/static/index.html"),
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
