package tunnel

import (
	"net/url"
	"reflect"
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
		want    []rule
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
			want: []rule{
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
			want: []rule{
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
			want: []rule{
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIngress([]byte(tt.args.rawYAML))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseIngress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIngress() = %v, want %v", got, tt.want)
			}
		})
	}
}
