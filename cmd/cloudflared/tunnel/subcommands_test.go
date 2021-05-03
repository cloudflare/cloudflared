package tunnel

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/tunnelstore"
)

func Test_fmtConnections(t *testing.T) {
	type args struct {
		connections []tunnelstore.Connection
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "empty",
			args: args{
				connections: []tunnelstore.Connection{},
			},
			want: "",
		},
		{
			name: "trivial",
			args: args{
				connections: []tunnelstore.Connection{
					{
						ColoName: "DFW",
						ID:       uuid.MustParse("ea550130-57fd-4463-aab1-752822231ddd"),
					},
				},
			},
			want: "1xDFW",
		},
		{
			name: "with a pending reconnect",
			args: args{
				connections: []tunnelstore.Connection{
					{
						ColoName:           "DFW",
						ID:                 uuid.MustParse("ea550130-57fd-4463-aab1-752822231ddd"),
						IsPendingReconnect: true,
					},
				},
			},
			want: "",
		},
		{
			name: "many colos",
			args: args{
				connections: []tunnelstore.Connection{
					{
						ColoName: "YRV",
						ID:       uuid.MustParse("ea550130-57fd-4463-aab1-752822231ddd"),
					},
					{
						ColoName: "DFW",
						ID:       uuid.MustParse("c13c0b3b-0fbf-453c-8169-a1990fced6d0"),
					},
					{
						ColoName: "ATL",
						ID:       uuid.MustParse("70c90639-e386-4e8d-9a4e-7f046d70e63f"),
					},
					{
						ColoName: "DFW",
						ID:       uuid.MustParse("30ad6251-0305-4635-a670-d3994f474981"),
					},
				},
			},
			want: "1xATL, 2xDFW, 1xYRV",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmtConnections(tt.args.connections, false); got != tt.want {
				t.Errorf("fmtConnections() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTunnelfilePath(t *testing.T) {
	tunnelID, err := uuid.Parse("f48d8918-bc23-4647-9d48-082c5b76de65")
	assert.NoError(t, err)
	originCertDir := filepath.Dir("~/.cloudflared/cert.pem")
	actual, err := tunnelFilePath(tunnelID, originCertDir)
	assert.NoError(t, err)
	homeDir, err := homedir.Dir()
	assert.NoError(t, err)
	expected := filepath.Join(homeDir, ".cloudflared", tunnelID.String()+".json")
	assert.Equal(t, expected, actual)
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "", want: false},
		{name: "-", want: false},
		{name: ".", want: false},
		{name: "a b", want: false},
		{name: "a+b", want: false},
		{name: "-ab", want: false},

		{name: "ab", want: true},
		{name: "ab-c", want: true},
		{name: "abc.def", want: true},
		{name: "_ab_c.-d-ef", want: true},
	}
	for _, tt := range tests {
		if got := validateName(tt.name, false); got != tt.want {
			t.Errorf("validateName() = %v, want %v", got, tt.want)
		}
	}
}

func Test_validateHostname(t *testing.T) {
	type args struct {
		s                      string
		allowWildcardSubdomain bool
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Normal",
			args: args{
				s:                      "example.com",
				allowWildcardSubdomain: true,
			},
			want: true,
		},
		{
			name: "wildcard subdomain for TUN-358",
			args: args{
				s:                      "*.ehrig.io",
				allowWildcardSubdomain: true,
			},
			want: true,
		},
		{
			name: "Misplaced wildcard",
			args: args{
				s:                      "subdomain.*.ehrig.io",
				allowWildcardSubdomain: true,
			},
		},
		{
			name: "Invalid domain",
			args: args{
				s:                      "..",
				allowWildcardSubdomain: true,
			},
		},
		{
			name: "Invalid domain",
			args: args{
				s: "..",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateHostname(tt.args.s, tt.args.allowWildcardSubdomain); got != tt.want {
				t.Errorf("validateHostname() = %v, want %v", got, tt.want)
			}
		})
	}
}
