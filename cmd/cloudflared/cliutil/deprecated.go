package cliutil

import (
	"fmt"

	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
)

const deprecatedFlagUsage = "DEPRECATED. No longer has any effect."

// NewDeprecatedStringFlag creates a hidden string flag whose usage identifies it as deprecated.
// Aliases and environment variables remain registered for backwards compatibility.
func NewDeprecatedStringFlag(flag *cli.StringFlag) *altsrc.StringFlag {
	flag.Hidden = true
	flag.Usage = deprecatedFlagUsage
	return altsrc.NewStringFlag(flag)
}

func RemovedCommand(name string) *cli.Command {
	return &cli.Command{
		Name: name,
		Action: func(context *cli.Context) error {
			return cli.Exit(
				fmt.Sprintf("%s command is no longer supported by cloudflared. Consult Cloudflare Tunnel documentation for possible alternative solutions.", name),
				-1,
			)
		},
		Description: fmt.Sprintf("%s is deprecated", name),
		Hidden:      true,
	}
}
