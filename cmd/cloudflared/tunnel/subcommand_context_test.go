package tunnel

import (
	"encoding/base64"
	"flag"
	"fmt"
	"reflect"
	"testing"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/tunnelstore"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
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

type mockFileSystem struct {
	rf  func(string) ([]byte, error)
	vfp func(string) bool
}

func (fs mockFileSystem) validFilePath(path string) bool {
	return fs.vfp(path)
}

func (fs mockFileSystem) readFile(filePath string) ([]byte, error) {
	return fs.rf(filePath)
}

func Test_subcommandContext_findCredentials(t *testing.T) {
	type fields struct {
		c                 *cli.Context
		logger            logger.Service
		isUIEnabled       bool
		fs                fileSystem
		tunnelstoreClient tunnelstore.Client
		userCredential    *userCredential
	}
	type args struct {
		tunnelID uuid.UUID
	}
	oldCertPath := "old_cert.json"
	newCertPath := "new_cert.json"
	accountTag := "0000d4d14e84bd4ae5a6a02e0000ac63"
	secret := []byte{211, 79, 177, 245, 179, 194, 152, 127, 140, 71, 18, 46, 183, 209, 10, 24, 192, 150, 55, 249, 211, 16, 167, 30, 113, 51, 152, 168, 72, 100, 205, 144}
	secretB64 := base64.StdEncoding.EncodeToString(secret)
	tunnelID := uuid.MustParse("df5ed608-b8b4-4109-89f3-9f2cf199df64")
	name := "mytunnel"

	fs := mockFileSystem{
		rf: func(filePath string) ([]byte, error) {
			if filePath == oldCertPath {
				// An old credentials file created before TUN-3581 added the new fields
				return []byte(fmt.Sprintf(`{"AccountTag":"%s","TunnelSecret":"%s"}`, accountTag, secretB64)), nil
			}
			if filePath == newCertPath {
				// A new credentials file created after TUN-3581 with its new fields.
				return []byte(fmt.Sprintf(`{"AccountTag":"%s","TunnelSecret":"%s","TunnelID":"%s","TunnelName":"%s"}`, accountTag, secretB64, tunnelID, name)), nil
			}
			return nil, errors.New("file not found")
		},
		vfp: func(string) bool { return true },
	}
	logger, err := logger.New()
	require.NoError(t, err)

	tests := []struct {
		name    string
		fields  fields
		args    args
		want    connection.Credentials
		wantErr bool
	}{
		{
			name: "Filepath given leads to old credentials file",
			fields: fields{
				logger: logger,
				fs:     fs,
				c: func() *cli.Context {
					flagSet := flag.NewFlagSet("test0", flag.PanicOnError)
					flagSet.String(CredFileFlag, oldCertPath, "")
					c := cli.NewContext(cli.NewApp(), flagSet, nil)
					err = c.Set(CredFileFlag, oldCertPath)
					return c
				}(),
			},
			args: args{
				tunnelID: tunnelID,
			},
			want: connection.Credentials{
				AccountTag:   accountTag,
				TunnelID:     tunnelID,
				TunnelSecret: secret,
			},
		},
		{
			name: "Filepath given leads to new credentials file",
			fields: fields{
				logger: logger,
				fs:     fs,
				c: func() *cli.Context {
					flagSet := flag.NewFlagSet("test0", flag.PanicOnError)
					flagSet.String(CredFileFlag, newCertPath, "")
					c := cli.NewContext(cli.NewApp(), flagSet, nil)
					err = c.Set(CredFileFlag, newCertPath)
					return c
				}(),
			},
			args: args{
				tunnelID: tunnelID,
			},
			want: connection.Credentials{
				AccountTag:   accountTag,
				TunnelID:     tunnelID,
				TunnelSecret: secret,
				TunnelName:   name,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &subcommandContext{
				c:                 tt.fields.c,
				logger:            tt.fields.logger,
				isUIEnabled:       tt.fields.isUIEnabled,
				fs:                tt.fields.fs,
				tunnelstoreClient: tt.fields.tunnelstoreClient,
				userCredential:    tt.fields.userCredential,
			}
			got, err := sc.findCredentials(tt.args.tunnelID)
			if (err != nil) != tt.wantErr {
				t.Errorf("subcommandContext.findCredentials() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subcommandContext.findCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}
