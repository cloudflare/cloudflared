package cliutil

import "gopkg.in/urfave/cli.v2"

// Ensures exit with error code if actionFunc returns an error
func ErrorHandler(actionFunc cli.ActionFunc) cli.ActionFunc {
	return func(ctx *cli.Context) error {
		err := actionFunc(ctx)
		if err != nil {
			// os.Exits with error code if err is cli.ExitCoder or cli.MultiError
			cli.HandleExitCoder(err)
			err = cli.Exit(err.Error(), 1)
		}
		return err
	}
}

