package tunnel

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cloudflare/cloudflared/tunnelstore"
	"github.com/mitchellh/go-homedir"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
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
			if got := fmtConnections(tt.args.connections); got != tt.want {
				t.Errorf("fmtConnections() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTunnelfilePath(t *testing.T) {
	originCertDir := filepath.Dir("~/.cloudflared/cert.pem")
	actual, err := tunnelFilePath("tunnel", originCertDir)
	assert.NoError(t, err)
	homeDir, err := homedir.Dir()
	assert.NoError(t, err)
	expected := fmt.Sprintf("%s/.cloudflared/tunnel.json", homeDir)
	assert.Equal(t, expected, actual)
}
