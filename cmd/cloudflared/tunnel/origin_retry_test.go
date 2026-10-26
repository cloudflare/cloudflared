package tunnel

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/ingress"
)

func TestOriginConnectRetryFlag(t *testing.T) {
	testCases := []struct {
		name     string
		env      string
		flag     string
		expected time.Duration
		wantErr  bool
	}{
		{name: "disabled by default"},
		{name: "environment", env: "500ms", expected: 500 * time.Millisecond},
		{name: "flag", flag: "500ms", expected: 500 * time.Millisecond},
		{name: "flag overrides environment", env: "500ms", flag: "2s", expected: 2 * time.Second},
		{name: "flag disables environment", env: "500ms", flag: "0s"},
		{name: "negative flag", flag: "-1s", wantErr: true},
		{name: "negative environment", env: "-1s", wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TUNNEL_PROXY_CONNECT_RETRY_TIMEOUT", tc.env)
			for _, origin := range [][]string{{"--url", "http://localhost:8000"}, {"--unix-socket", "/tmp/app.sock"}} {
				t.Run(origin[0], func(t *testing.T) {
					app := cli.NewApp()
					app.Flags = configureProxyFlags(false)
					app.Action = func(c *cli.Context) error {
						log := zerolog.Nop()
						rules, err := ingress.ParseIngressFromConfigAndCLI(&config.Configuration{}, c, &log)
						if err != nil {
							return err
						}
						require.Len(t, rules.Rules, 1)
						require.Equal(t, tc.expected, rules.Rules[0].Config.ConnectRetryTimeout.Duration)
						return nil
					}
					args := append([]string{"cloudflared"}, origin...)
					if tc.flag != "" {
						args = append(args, "--proxy-connect-retry-timeout", tc.flag)
					}
					err := app.Run(args)
					if tc.wantErr {
						require.ErrorContains(t, err, "connectRetryTimeout must not be negative")
						return
					}
					require.NoError(t, err)
				})
			}
		})
	}
}
