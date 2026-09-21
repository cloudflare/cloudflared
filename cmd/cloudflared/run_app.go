package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
)

// runAppAndExit runs the CLI app and exits with a non-zero status when it
// fails. Flag and env-var parse errors (e.g. an invalid TUNNEL_GRACE_PERIOD)
// are returned by app.Run without being printed, so ignoring the error used
// to make cloudflared exit 0 with no output (see #1606). Print the error to
// stderr and exit 1 so scripts and service managers can detect the failure.
func runAppAndExit(app *cli.App) {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
