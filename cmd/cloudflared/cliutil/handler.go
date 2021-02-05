package cliutil

import (
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
)

func Action(actionFunc cli.ActionFunc) cli.ActionFunc {
	return WithErrorHandler(func(c *cli.Context) error {
		if err := setFlagsFromConfigFile(c); err != nil {
			return err
		}
		return actionFunc(c)
	})
}

func setFlagsFromConfigFile(c *cli.Context) error {
	const errorExitCode = 1
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	inputSource, err := config.ReadConfigFile(c, log)
	if err != nil {
		if err == config.ErrNoConfigFile {
			return nil
		}
		return cli.Exit(err, errorExitCode)
	}

	if err := applyConfig(c, inputSource); err != nil {
		return cli.Exit(err, errorExitCode)
	}
	return nil
}

func applyConfig(c *cli.Context, inputSource altsrc.InputSourceContext) error {
	for _, context := range c.Lineage() {
		if context.Command == nil {
			// we've reached the placeholder root context not associated with the app
			break
		}
		targetFlags := context.Command.Flags
		if context.Command.Name == "" {
			// commands that define child subcommands are executed as if they were an app
			targetFlags = c.App.Flags
		}
		if err := altsrc.ApplyInputSourceValues(context, inputSource, targetFlags); err != nil {
			return err
		}
	}
	return nil
}
