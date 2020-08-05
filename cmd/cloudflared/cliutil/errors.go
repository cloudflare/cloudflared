package cliutil

import (
	"fmt"
	"log"

	"github.com/cloudflare/cloudflared/logger"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
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
		logger.SharedWriteManager.Shutdown()
		return err
	}
}

// PrintLoggerSetupError returns an error to stdout to notify when a logger can't start
func PrintLoggerSetupError(msg string, err error) error {
	l, le := logger.New()
	if le != nil {
		log.Printf("%s: %s", msg, err)
	} else {
		l.Errorf("%s: %s", msg, err)
	}

	return errors.Wrap(err, msg)
}
