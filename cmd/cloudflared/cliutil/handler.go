package cliutil

import (
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
)

func Action(actionFunc cli.ActionFunc) cli.ActionFunc {
	return WithErrorHandler(actionFunc)
}

func ConfiguredAction(actionFunc cli.ActionFunc) cli.ActionFunc {
	// Adapt actionFunc to the type signature required by ConfiguredActionWithWarnings
	f := func(context *cli.Context, _ string) error {
		return actionFunc(context)
	}

	return ConfiguredActionWithWarnings(f)
}

// Just like ConfiguredAction, but accepts a second parameter with configuration warnings.
func ConfiguredActionWithWarnings(actionFunc func(*cli.Context, string) error) cli.ActionFunc {
	return WithErrorHandler(func(c *cli.Context) error {
		warnings, err := setFlagsFromConfigFile(c)
		if err != nil {
			return err
		}
		return actionFunc(c, warnings)
	})
}

func setFlagsFromConfigFile(c *cli.Context) (configWarnings string, err error) {
	const errorExitCode = 1
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	inputSource, warnings, err := config.ReadConfigFile(c, log)
	if err != nil {
		if err == config.ErrNoConfigFile {
			return "", nil
		}
		return "", cli.Exit(err, errorExitCode)
	}

	if err := altsrc.ApplyInputSource(c, inputSource); err != nil {
		return "", cli.Exit(err, errorExitCode)
	}
	return warnings, nil
}
