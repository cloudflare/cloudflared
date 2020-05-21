package cliutil

import (
	"fmt"

	"gopkg.in/urfave/cli.v2"
)

type usageError string

func (ue usageError) Error() string {
	return string(ue)
}

func UsageError(format string, args ...interface{}) error {
	if len(args) == 0 {
		return usageError(format)
	} else {
		msg := fmt.Sprintf(format, args...)
		return usageError(msg)
	}
}

// Ensures exit with error code if actionFunc returns an error
func ErrorHandler(actionFunc cli.ActionFunc) cli.ActionFunc {
	return func(ctx *cli.Context) error {
		err := actionFunc(ctx)
		if err != nil {
			if _, ok := err.(usageError); ok {
				msg := fmt.Sprintf("%s\nSee 'cloudflared %s --help'.", err.Error(), ctx.Command.FullName())
				return cli.Exit(msg, -1)
			}
			// os.Exits with error code if err is cli.ExitCoder or cli.MultiError
			cli.HandleExitCoder(err)
			err = cli.Exit(err.Error(), 1)
		}
		return err
	}
}
