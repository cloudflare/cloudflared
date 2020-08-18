package tunnel

import (
	"reflect"
	"testing"

	"github.com/cloudflare/cloudflared/tunnelstore"
	"github.com/google/uuid"
)

func Test_findIDs(t *testing.T) {
	type args struct {
		tunnels []*tunnelstore.Tunnel
		inputs  []string
	}
	tests := []struct {
		name    string
		args    args
		want    []uuid.UUID
		wantErr bool
	}{
		{
			name: "input not found",
			args: args{
				inputs: []string{"asdf"},
			},
			wantErr: true,
		},
		{
			name: "only UUID",
			args: args{
				inputs: []string{"a8398a0b-876d-48ed-b609-3fcfd67a4950"},
			},
			want: []uuid.UUID{uuid.MustParse("a8398a0b-876d-48ed-b609-3fcfd67a4950")},
		},
		{
			name: "only name",
			args: args{
				tunnels: []*tunnelstore.Tunnel{
					{
						ID:   uuid.MustParse("a8398a0b-876d-48ed-b609-3fcfd67a4950"),
						Name: "tunnel1",
					},
				},
				inputs: []string{"tunnel1"},
			},
			want: []uuid.UUID{uuid.MustParse("a8398a0b-876d-48ed-b609-3fcfd67a4950")},
		},
		{
			name: "both UUID and name",
			args: args{
				tunnels: []*tunnelstore.Tunnel{
					{
						ID:   uuid.MustParse("a8398a0b-876d-48ed-b609-3fcfd67a4950"),
						Name: "tunnel1",
					},
					{
						ID:   uuid.MustParse("bf028b68-744f-466e-97f8-c46161d80aa5"),
						Name: "tunnel2",
					},
				},
				inputs: []string{"tunnel1", "bf028b68-744f-466e-97f8-c46161d80aa5"},
			},
			want: []uuid.UUID{
				uuid.MustParse("a8398a0b-876d-48ed-b609-3fcfd67a4950"),
				uuid.MustParse("bf028b68-744f-466e-97f8-c46161d80aa5"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findIDs(tt.args.tunnels, tt.args.inputs)
			if (err != nil) != tt.wantErr {
				t.Errorf("findIDs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findIDs() = %v, want %v", got, tt.want)
			}
		})
	}
}
