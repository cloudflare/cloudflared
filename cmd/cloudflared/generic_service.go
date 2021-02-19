// +build !windows,!darwin,!linux

package main

import (
	"os"

	cli "github.com/urfave/cli/v2"
)

func runApp(app *cli.App, graceShutdownC chan struct{}) {
	app.Run(os.Args)
}
