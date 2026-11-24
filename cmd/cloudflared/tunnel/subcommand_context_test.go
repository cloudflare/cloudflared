package tunnel

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cfapi"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/credentials"
)

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
		log               *zerolog.Logger
		fs                fileSystem
		tunnelstoreClient cfapi.Client
		userCredential    *credentials.User
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
	log := zerolog.Nop()

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
				log: &log,
				fs:  fs,
				c: func() *cli.Context {
					flagSet := flag.NewFlagSet("test0", flag.PanicOnError)
					flagSet.String(CredFileFlag, oldCertPath, "")
					c := cli.NewContext(cli.NewApp(), flagSet, nil)
					_ = c.Set(CredFileFlag, oldCertPath)
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
				log: &log,
				fs:  fs,
				c: func() *cli.Context {
					flagSet := flag.NewFlagSet("test0", flag.PanicOnError)
					flagSet.String(CredFileFlag, newCertPath, "")
					c := cli.NewContext(cli.NewApp(), flagSet, nil)
					_ = c.Set(CredFileFlag, newCertPath)
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
			name: "TUNNEL_CRED_CONTENTS given contains old credentials contents",
			fields: fields{
				log: &log,
				fs:  fs,
				c: func() *cli.Context {
					flagSet := flag.NewFlagSet("test0", flag.PanicOnError)
					flagSet.String(CredContentsFlag, "", "")
					c := cli.NewContext(cli.NewApp(), flagSet, nil)
					_ = c.Set(CredContentsFlag, fmt.Sprintf(`{"AccountTag":"%s","TunnelSecret":"%s"}`, accountTag, secretB64))
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
			name: "TUNNEL_CRED_CONTENTS given contains new credentials contents",
			fields: fields{
				log: &log,
				fs:  fs,
				c: func() *cli.Context {
					flagSet := flag.NewFlagSet("test0", flag.PanicOnError)
					flagSet.String(CredContentsFlag, "", "")
					c := cli.NewContext(cli.NewApp(), flagSet, nil)
					_ = c.Set(CredContentsFlag, fmt.Sprintf(`{"AccountTag":"%s","TunnelSecret":"%s","TunnelID":"%s","TunnelName":"%s"}`, accountTag, secretB64, tunnelID, name))
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &subcommandContext{
				c:                 tt.fields.c,
				log:               tt.fields.log,
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

type deleteMockTunnelStore struct {
	cfapi.Client
	mockTunnels      map[uuid.UUID]mockTunnelBehaviour
	deletedTunnelIDs []uuid.UUID
}

type mockTunnelBehaviour struct {
	tunnel     cfapi.Tunnel
	deleteErr  error
	cleanupErr error
}

func newDeleteMockTunnelStore(tunnels ...mockTunnelBehaviour) *deleteMockTunnelStore {
	mockTunnels := make(map[uuid.UUID]mockTunnelBehaviour)
	for _, tunnel := range tunnels {
		mockTunnels[tunnel.tunnel.ID] = tunnel
	}
	return &deleteMockTunnelStore{
		mockTunnels:      mockTunnels,
		deletedTunnelIDs: make([]uuid.UUID, 0),
	}
}

func (d *deleteMockTunnelStore) GetTunnel(tunnelID uuid.UUID) (*cfapi.Tunnel, error) {
	tunnel, ok := d.mockTunnels[tunnelID]
	if !ok {
		return nil, fmt.Errorf("Couldn't find tunnel: %v", tunnelID)
	}
	return &tunnel.tunnel, nil
}

func (d *deleteMockTunnelStore) GetTunnelToken(tunnelID uuid.UUID) (string, error) {
	return "token", nil
}

func (d *deleteMockTunnelStore) DeleteTunnel(tunnelID uuid.UUID, cascade bool) error {
	tunnel, ok := d.mockTunnels[tunnelID]
	if !ok {
		return fmt.Errorf("Couldn't find tunnel: %v", tunnelID)
	}

	if tunnel.deleteErr != nil {
		return tunnel.deleteErr
	}

	d.deletedTunnelIDs = append(d.deletedTunnelIDs, tunnelID)
	tunnel.tunnel.DeletedAt = time.Now()
	delete(d.mockTunnels, tunnelID)
	return nil
}

func (d *deleteMockTunnelStore) CleanupConnections(tunnelID uuid.UUID, _ *cfapi.CleanupParams) error {
	tunnel, ok := d.mockTunnels[tunnelID]
	if !ok {
		return fmt.Errorf("Couldn't find tunnel: %v", tunnelID)
	}
	return tunnel.cleanupErr
}

func Test_subcommandContext_Delete(t *testing.T) {
	type fields struct {
		c                 *cli.Context
		log               *zerolog.Logger
		fs                fileSystem
		tunnelstoreClient *deleteMockTunnelStore
		userCredential    *credentials.User
	}
	type args struct {
		tunnelIDs []uuid.UUID
	}
	newCertPath := "new_cert.json"
	tunnelID1 := uuid.MustParse("df5ed608-b8b4-4109-89f3-9f2cf199df64")
	tunnelID2 := uuid.MustParse("af5ed608-b8b4-4109-89f3-9f2cf199df64")
	log := zerolog.Nop()

	var tests = []struct {
		name    string
		fields  fields
		args    args
		want    []uuid.UUID
		wantErr bool
	}{
		{
			name: "clean up continues if credentials are not found",
			fields: fields{
				log: &log,
				fs: mockFileSystem{
					rf: func(filePath string) ([]byte, error) {
						return nil, errors.New("file not found")
					},
					vfp: func(string) bool { return true },
				},
				c: func() *cli.Context {
					flagSet := flag.NewFlagSet("test0", flag.PanicOnError)
					flagSet.String(CredFileFlag, newCertPath, "")
					c := cli.NewContext(cli.NewApp(), flagSet, nil)
					_ = c.Set(CredFileFlag, newCertPath)
					return c
				}(),
				tunnelstoreClient: newDeleteMockTunnelStore(
					mockTunnelBehaviour{
						tunnel: cfapi.Tunnel{ID: tunnelID1},
					},
					mockTunnelBehaviour{
						tunnel: cfapi.Tunnel{ID: tunnelID2},
					},
				),
			},

			args: args{
				tunnelIDs: []uuid.UUID{tunnelID1, tunnelID2},
			},
			want: []uuid.UUID{tunnelID1, tunnelID2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &subcommandContext{
				c:                 tt.fields.c,
				log:               tt.fields.log,
				fs:                tt.fields.fs,
				tunnelstoreClient: tt.fields.tunnelstoreClient,
				userCredential:    tt.fields.userCredential,
			}
			err := sc.delete(tt.args.tunnelIDs)
			if (err != nil) != tt.wantErr {
				t.Errorf("subcommandContext.findCredentials() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			got := tt.fields.tunnelstoreClient.deletedTunnelIDs
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subcommandContext.findCredentials() = %v, want %v", got, tt.want)
				return
			}
		})
	}
}

func Test_subcommandContext_ValidateIngressCommand(t *testing.T) {
	testValidConfig := `{ "warp-routing": {"enabled": true},  "originRequest" : {"connectTimeout": 10}, "ingress" : [ {"hostname": "test", "service": "https://localhost:8000" } , {"service": "http_status:404"} ]}`
	testInvalidConfig := `{ "ingress" : [ {"hostname": "test", "service": "localhost:8000" } , {"service": "http_status:invalid_status"} ]}`
	invalidConfigErr := errors.New("Validation failed: localhost:8000 is an invalid address, please make sure it has a scheme and a hostname")

	newGlobalConf := func(rules ...config.UnvalidatedIngressRule) *config.Configuration {
		conf := &config.Configuration{Ingress: rules}
		conf.SetSource("/etc/cloudflared/config.yml")
		return conf
	}

	var tests = []struct {
		name        string
		stdin       io.Reader
		globalConf  *config.Configuration
		c           *cli.Context
		expectedErr error
	}{
		{
			name:  "read a valid configuration from the command line",
			stdin: strings.NewReader(""),
			c: func() *cli.Context {
				flagSet := flag.NewFlagSet("json", flag.PanicOnError)
				flagSet.String(ingressDataJSONArgvFlagName, testValidConfig, "")
				c := cli.NewContext(cli.NewApp(), flagSet, nil)
				_ = c.Set(ingressDataJSONArgvFlagName, testValidConfig)
				return c
			}(),
		},
		{
			name:  "read a valid configuration from stdin when no flags are provided",
			stdin: strings.NewReader(testValidConfig),
			c: func() *cli.Context {
				flagSet := flag.NewFlagSet("no-flags", flag.PanicOnError)
				c := cli.NewContext(cli.NewApp(), flagSet, nil)
				return c
			}(),
		},
		{
			name:  "read an invalid configuration with multiple mistakes from the command line",
			stdin: strings.NewReader(""),
			c: func() *cli.Context {
				flagSet := flag.NewFlagSet("json", flag.PanicOnError)
				flagSet.String(ingressDataJSONArgvFlagName, testInvalidConfig, "")
				c := cli.NewContext(cli.NewApp(), flagSet, nil)
				_ = c.Set(ingressDataJSONArgvFlagName, testInvalidConfig)
				return c
			}(),
			expectedErr: invalidConfigErr,
		},
		{
			name:  "read an invalid configuration with multiple mistakes from stdin when no flags are provided",
			stdin: strings.NewReader(testInvalidConfig),
			c: func() *cli.Context {
				flagSet := flag.NewFlagSet("no-flags", flag.PanicOnError)
				c := cli.NewContext(cli.NewApp(), flagSet, nil)
				return c
			}(),
			expectedErr: invalidConfigErr,
		},
		{
			name:  "stdin read error",
			stdin: iotest.ErrReader(errors.New("stdin error")),
			c: func() *cli.Context {
				flagSet := flag.NewFlagSet("no-flags", flag.PanicOnError)
				return cli.NewContext(cli.NewApp(), flagSet, nil)
			}(),
			expectedErr: errors.New("read stdin: stdin error"),
		},
		{
			name:  "read a valid configuration from the global config without reading stdin",
			stdin: iotest.ErrReader(errors.New("stdin must not be read")),
			globalConf: newGlobalConf(
				config.UnvalidatedIngressRule{Hostname: "test", Service: "https://localhost:8000"},
				config.UnvalidatedIngressRule{Service: "http_status:404"},
			),
			c: func() *cli.Context {
				flagSet := flag.NewFlagSet("no-flags", flag.PanicOnError)
				return cli.NewContext(cli.NewApp(), flagSet, nil)
			}(),
		},
		{
			name:  "read an invalid configuration from the global config without reading stdin",
			stdin: iotest.ErrReader(errors.New("stdin must not be read")),
			globalConf: newGlobalConf(
				config.UnvalidatedIngressRule{Hostname: "test", Service: "localhost:8000"},
				config.UnvalidatedIngressRule{Service: "http_status:404"},
			),
			c: func() *cli.Context {
				flagSet := flag.NewFlagSet("no-flags", flag.PanicOnError)
				return cli.NewContext(cli.NewApp(), flagSet, nil)
			}(),
			expectedErr: invalidConfigErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globalConf := tt.globalConf
			if globalConf == nil {
				globalConf = &config.Configuration{}
			}

			err := validateIngress(tt.c, "", globalConf, tt.stdin)
			if tt.expectedErr != nil {
				assert.Equal(t, tt.expectedErr.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

type findIDMockTunnelStore struct {
	cfapi.Client
	listTunnelsFunc func(filter *cfapi.TunnelFilter) ([]*cfapi.Tunnel, error)
}

func (m *findIDMockTunnelStore) ListTunnels(filter *cfapi.TunnelFilter) ([]*cfapi.Tunnel, error) {
	if m.listTunnelsFunc != nil {
		return m.listTunnelsFunc(filter)
	}
	return nil, nil
}

func Test_subcommandContext_findID(t *testing.T) {
	tunnelIDA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tunnelIDB := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	credFileA := "tunnel_a_cred.json"
	log := zerolog.Nop()

	fsWithCredA := mockFileSystem{
		rf: func(filePath string) ([]byte, error) {
			if filePath == credFileA {
				return []byte(fmt.Sprintf(`{"AccountTag":"tag","TunnelSecret":"c2VjcmV0","TunnelID":"%s"}`, tunnelIDA)), nil
			}
			return nil, errors.New("file not found")
		},
		vfp: func(string) bool { return true },
	}

	makeContextWithCred := func(credPath string) *cli.Context {
		flagSet := flag.NewFlagSet("test", flag.PanicOnError)
		flagSet.String(CredFileFlag, credPath, "")
		c := cli.NewContext(cli.NewApp(), flagSet, nil)
		_ = c.Set(CredFileFlag, credPath)
		return c
	}

	tests := []struct {
		name              string
		input             string
		c                 *cli.Context
		fs                fileSystem
		tunnelstoreClient cfapi.Client
		want              uuid.UUID
		wantErr           bool
		wantErrMsg        string
	}{
		{
			name:  "UUID input is parsed directly without querying Tunnelstore or credentials",
			input: tunnelIDA.String(),
			want:  tunnelIDA,
		},
		{
			name:  "Tunnel name resolves via Tunnelstore even if a different credentials file is configured",
			input: "tunnel-b",
			c:     makeContextWithCred(credFileA),
			fs:    fsWithCredA,
			tunnelstoreClient: &findIDMockTunnelStore{
				listTunnelsFunc: func(filter *cfapi.TunnelFilter) ([]*cfapi.Tunnel, error) {
					return []*cfapi.Tunnel{
						{ID: tunnelIDB, Name: "tunnel-b"},
					}, nil
				},
			},
			want: tunnelIDB,
		},
		{
			name:  "Tunnel name not found in Tunnelstore returns error and does not misroute to configured credentials file",
			input: "non-existent-tunnel",
			c:     makeContextWithCred(credFileA),
			fs:    fsWithCredA,
			tunnelstoreClient: &findIDMockTunnelStore{
				listTunnelsFunc: func(filter *cfapi.TunnelFilter) ([]*cfapi.Tunnel, error) {
					return []*cfapi.Tunnel{}, nil
				},
			},
			wantErr:    true,
			wantErrMsg: "non-existent-tunnel is neither the ID nor the name of any of your tunnels",
		},
		{
			name:  "When API credentials are available but Tunnelstore returns error, returns error without falling back to credentials file",
			input: "tunnel-b",
			c:     makeContextWithCred(credFileA),
			fs:    fsWithCredA,
			tunnelstoreClient: &findIDMockTunnelStore{
				listTunnelsFunc: func(filter *cfapi.TunnelFilter) ([]*cfapi.Tunnel, error) {
					return nil, errors.New("cannot reach tunnelstore")
				},
			},
			wantErr:    true,
			wantErrMsg: "cannot reach tunnelstore",
		},
		{
			name:  "When API credentials are unavailable, falls back to reading configured credentials file",
			input: "tunnel-a",
			c:     makeContextWithCred(credFileA),
			fs:    fsWithCredA,
			want:  tunnelIDA,
		},
		{
			name:  "When API credentials are unavailable and credentials file is missing, returns error",
			input: "tunnel-a",
			c:     makeContextWithCred("missing.json"),
			fs: mockFileSystem{
				rf:  func(string) ([]byte, error) { return nil, errors.New("file not found") },
				vfp: func(string) bool { return false },
			},
			wantErr:    true,
			wantErrMsg: "Error locating origin cert",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &subcommandContext{
				c:                 tt.c,
				log:               &log,
				fs:                tt.fs,
				tunnelstoreClient: tt.tunnelstoreClient,
			}
			if sc.c == nil {
				sc.c = makeContextWithCred("")
			}
			got, err := sc.findID(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.wantErrMsg != "" {
					assert.Contains(t, err.Error(), tt.wantErrMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
