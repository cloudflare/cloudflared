package proxydns

import (
	"errors"

	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
)

const removedMessage = "dns-proxy feature is not supported, since version 2026.2.0"

func Command() *cli.Command {
	return &cli.Command{
		Name:            "proxy-dns",
		Action:          cliutil.ConfiguredAction(Run),
		Usage:           removedMessage,
		SkipFlagParsing: true,
	}
}

func Run(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	err := errors.New(removedMessage)
	log.Err(err).Msg("DNS Proxy is no longer supported")

	return err
}

// Old flags used by the proxy-dns command, only kept to not break any script that might be setting these flags
func ConfigureProxyDNSFlags(shouldHide bool) []cli.Flag {
	return []cli.Flag{
		altsrc.NewBoolFlag(&cli.BoolFlag{
			Name: "proxy-dns",
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name: "proxy-dns-port",
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name: "proxy-dns-address",
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name: "proxy-dns-upstream",
		}),
		altsrc.NewIntFlag(&cli.IntFlag{
			Name: "proxy-dns-max-upstream-conns",
		}),
		altsrc.NewStringSliceFlag(&cli.StringSliceFlag{
			Name: "proxy-dns-bootstrap",
		}),
	}
}
