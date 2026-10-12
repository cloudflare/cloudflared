package access

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	cfdflags "github.com/cloudflare/cloudflared/cmd/cloudflared/flags"
)

func TestAccessCommandHasPidfileFlag(t *testing.T) {
	t.Parallel()

	cmds := Commands()
	require.NotEmpty(t, cmds)

	var accessCmd *cli.Command
	for _, cmd := range cmds {
		if cmd.Name == "access" {
			accessCmd = cmd
			break
		}
	}
	require.NotNil(t, accessCmd)

	var accessPidFlag *cli.StringFlag
	for _, f := range accessCmd.Flags {
		if sf, ok := f.(*cli.StringFlag); ok && sf.Name == cfdflags.Pidfile {
			accessPidFlag = sf
			break
		}
	}
	require.NotNil(t, accessPidFlag, "access command should have --pidfile flag")
	assert.Contains(t, accessPidFlag.EnvVars, "TUNNEL_PIDFILE")

	var tcpCmd *cli.Command
	for _, sub := range accessCmd.Subcommands {
		if sub.Name == "tcp" {
			tcpCmd = sub
			break
		}
	}
	require.NotNil(t, tcpCmd)

	var tcpPidFlag *cli.StringFlag
	for _, f := range tcpCmd.Flags {
		if sf, ok := f.(*cli.StringFlag); ok && sf.Name == cfdflags.Pidfile {
			tcpPidFlag = sf
			break
		}
	}
	require.NotNil(t, tcpPidFlag, "access tcp subcommand should have --pidfile flag")
	assert.Contains(t, tcpPidFlag.EnvVars, "TUNNEL_PIDFILE")
}
