package main

import (
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
)

func buildArgsForToken(c *cli.Context, log *zerolog.Logger) ([]string, error) {
	token := c.Args().First()
	if _, err := tunnel.ParseToken(token); err != nil {
		return nil, cliutil.UsageError("Provided tunnel token is not valid (%s).", err)
	}

	return []string{
		"tunnel", "run", "--token", token,
	}, nil
}

func getServiceExtraArgsFromCliArgs(c *cli.Context, log *zerolog.Logger) ([]string, error) {
	if c.NArg() > 0 {
		// currently, we only support extra args for token
		return buildArgsForToken(c, log)
	} else {
		// empty extra args
		return make([]string, 0), nil
	}
}
