package tunnel

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	cfdflags "github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
)

func TestLogClientOptionsRedactsAllowedMail(t *testing.T) {
	t.Parallel()

	const recipient = "private@example.com"
	flagSet := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
	allowedMailFlag := &cli.StringSliceFlag{Name: cfdflags.AllowedMail}
	require.NoError(t, allowedMailFlag.Apply(flagSet))
	require.NoError(t, flagSet.Parse([]string{"--allowed-mail", recipient}))
	ctx := cli.NewContext(cli.NewApp(), flagSet, nil)
	var output bytes.Buffer
	log := zerolog.New(&output)

	logClientOptions(ctx, &log)

	assert.Contains(t, output.String(), cfdflags.AllowedMail)
	assert.Contains(t, output.String(), secretValue)
	assert.NotContains(t, output.String(), recipient)
}

func TestNamedTunnelRejectsAllowedMailFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action cli.ActionFunc
		args   []string
	}{
		{
			name:   "ad hoc named tunnel",
			action: TunnelCommand,
			args:   []string{"--name", "named", "--url", "http://localhost:8080", "--allowed-mail", "private@example.com"},
		},
		{
			name:   "run named tunnel",
			action: runCommand,
			args:   []string{"--allowed-mail", "private@example.com", "named"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			flagSet := flag.NewFlagSet(t.Name(), flag.ContinueOnError)
			for _, cliFlag := range []cli.Flag{
				&cli.StringSliceFlag{Name: cfdflags.AllowedMail},
				&cli.StringFlag{Name: cfdflags.Name},
				&cli.StringFlag{Name: "url"},
			} {
				require.NoError(t, cliFlag.Apply(flagSet))
			}
			require.NoError(t, flagSet.Parse(test.args))

			ctx := cli.NewContext(cli.NewApp(), flagSet, nil)
			err := test.action(ctx)
			require.ErrorContains(t, err, "--allowed-mail is only supported for Quick Tunnels")
		})
	}
}

func TestNamedTunnelRejectsAllowedMailFromConfig(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		args       []string
	}{
		{
			name: "ad hoc named tunnel",
			configYAML: `name: named
url: http://localhost:8080
allowed-mail:
  - private@example.com
`,
		},
		{
			name: "run named tunnel",
			configYAML: `allowed-mail:
  - private@example.com
`,
			args: []string{"run", "named"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.yml")
			require.NoError(t, os.WriteFile(configPath, []byte(test.configYAML), 0600))

			app := cli.NewApp()
			app.ExitErrHandler = func(*cli.Context, error) {}
			app.Commands = []*cli.Command{buildTunnelCommand([]*cli.Command{buildRunCommand()})}
			args := make([]string, 0, 4+len(test.args))
			args = append(args, "cloudflared", "tunnel", "--config", configPath)
			args = append(args, test.args...)

			err := app.Run(args)
			require.ErrorContains(t, err, "--allowed-mail is only supported for Quick Tunnels")
		})
	}
}

func TestHostnameFromURI(t *testing.T) {
	assert.Equal(t, "awesome.warptunnels.horse:22", hostnameFromURI("ssh://awesome.warptunnels.horse:22"))
	assert.Equal(t, "awesome.warptunnels.horse:22", hostnameFromURI("ssh://awesome.warptunnels.horse"))
	assert.Equal(t, "awesome.warptunnels.horse:2222", hostnameFromURI("ssh://awesome.warptunnels.horse:2222"))
	assert.Equal(t, "localhost:3389", hostnameFromURI("rdp://localhost"))
	assert.Equal(t, "localhost:3390", hostnameFromURI("rdp://localhost:3390"))
	assert.Empty(t, hostnameFromURI("trash"))
	assert.Empty(t, hostnameFromURI("https://awesomesauce.com"))
}
