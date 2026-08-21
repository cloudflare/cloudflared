package access

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/token"
)

// accessTimeoutDurationFlag pulls the real --access-timeout flag definition
// off of the "access" command, so precedence is tested against production
// wiring rather than a re-declared copy of it.
func accessTimeoutDurationFlag(t *testing.T) *cli.DurationFlag {
	t.Helper()
	cmds := Commands()
	require.Len(t, cmds, 1)
	for _, f := range cmds[0].Flags {
		if df, ok := f.(*cli.DurationFlag); ok && df.Name == accessTimeoutFlag {
			return df
		}
	}
	t.Fatal("access-timeout flag not registered on the access command")
	return nil
}

func parseAccessTimeout(t *testing.T, args []string) time.Duration {
	t.Helper()
	set := flag.NewFlagSet("access", flag.ContinueOnError)
	require.NoError(t, accessTimeoutDurationFlag(t).Apply(set))
	require.NoError(t, set.Parse(args))
	ctx := cli.NewContext(cli.NewApp(), set, nil)
	return ctx.Duration(accessTimeoutFlag)
}

func TestAccessTimeoutFlag_DefaultPreservesCurrentBehavior(t *testing.T) {
	got := parseAccessTimeout(t, nil)
	assert.Equal(t, token.DefaultAccessTimeout, got)
	assert.Equal(t, 7*time.Second, got)
}

func TestAccessTimeoutFlag_EnvVarOverridesDefault(t *testing.T) {
	t.Setenv("TUNNEL_ACCESS_TIMEOUT", "30s")
	got := parseAccessTimeout(t, nil)
	assert.Equal(t, 30*time.Second, got)
}

func TestAccessTimeoutFlag_FlagOverridesEnvVar(t *testing.T) {
	t.Setenv("TUNNEL_ACCESS_TIMEOUT", "30s")
	got := parseAccessTimeout(t, []string{"--access-timeout", "45s"})
	assert.Equal(t, 45*time.Second, got)
}
