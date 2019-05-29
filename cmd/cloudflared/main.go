package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/access"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
	"github.com/cloudflare/cloudflared/log"
	"github.com/cloudflare/cloudflared/metrics"

	raven "github.com/getsentry/raven-go"
	homedir "github.com/mitchellh/go-homedir"
	cli "gopkg.in/urfave/cli.v2"

	"github.com/pkg/errors"
)

const (
	versionText = "Print the version"
)

var (
	Version   = "DEV"
	BuildTime = "unknown"
	logger    = log.CreateLogger()
	// Mostly network errors that we don't want reported back to Sentry, this is done by substring match.
	ignoredErrors = []string{
		"connection reset by peer",
		"An existing connection was forcibly closed by the remote host.",
		"use of closed connection",
		"You need to enable Argo Smart Routing",
		"3001 connection closed",
		"3002 connection dropped",
		"rpc exception: dial tcp",
		"rpc exception: EOF",
	}
)

func main() {
	metrics.RegisterBuildInfo(BuildTime, Version)
	raven.SetRelease(Version)

	// Force shutdown channel used by the app. When closed, app must terminate.
	// Windows service manager closes this channel when it receives shutdown command.
	shutdownC := make(chan struct{})
	// Graceful shutdown channel used by the app. When closed, app must terminate.
	// Windows service manager closes this channel when it receives stop command.
	graceShutdownC := make(chan struct{})

	cli.VersionFlag = &cli.BoolFlag{
		Name:    "version",
		Aliases: []string{"v", "V"},
		Usage:   versionText,
	}

	app := &cli.App{}
	app.Name = "cloudflared"
	app.Usage = "Cloudflare's command-line tool and agent"
	app.ArgsUsage = "origin-url"
	app.Copyright = fmt.Sprintf(
		`(c) %d Cloudflare Inc.
   Your installation of cloudflared software constitutes a symbol of your signature indicating that you accept
   the terms of the Cloudflare License (https://developers.cloudflare.com/argo-tunnel/license/),
   Terms (https://www.cloudflare.com/terms/) and Privacy Policy (https://www.cloudflare.com/privacypolicy/).`,
		time.Now().Year(),
	)
	app.Version = fmt.Sprintf("%s (built %s)", Version, BuildTime)
	app.Description = `cloudflared connects your machine or user identity to Cloudflare's global network.
	You can use it to authenticate a session to reach an API behind Access, route web traffic to this machine,
	and configure access control.`
	app.Flags = flags()
	app.Action = action(Version, shutdownC, graceShutdownC)
	app.Before = tunnel.Before
	app.Commands = commands(cli.ShowVersion)

	tunnel.Init(Version, shutdownC, graceShutdownC) // we need this to support the tunnel sub command...
	access.Init(shutdownC, graceShutdownC)
	runApp(app, shutdownC, graceShutdownC)
}

func commands(version func(c *cli.Context)) []*cli.Command {
	cmds := []*cli.Command{
		{
			Name:      "update",
			Action:    updater.Update,
			Usage:     "Update the agent if a new version exists",
			ArgsUsage: " ",
			Description: `Looks for a new version on the official download server.
If a new version exists, updates the agent binary and quits.
Otherwise, does nothing.

To determine if an update happened in a script, check for error code 64.`,
		},
		{
			Name: "version",
			Action: func(c *cli.Context) (err error) {
				version(c)
				return nil
			},
			Usage:       versionText,
			Description: versionText,
		},
	}
	cmds = append(cmds, tunnel.Commands()...)
	cmds = append(cmds, access.Commands()...)
	return cmds
}

func flags() []cli.Flag {
	flags := tunnel.Flags()
	return append(flags, access.Flags()...)
}

func isEmptyInvocation(c *cli.Context) bool {
	return c.NArg() == 0 && c.NumFlags() == 0
}

func action(version string, shutdownC, graceShutdownC chan struct{}) cli.ActionFunc {
	return func(c *cli.Context) (err error) {
		if isEmptyInvocation(c) {
			cli.ShowAppHelpAndExit(c, 1)
		}
		tags := make(map[string]string)
		tags["hostname"] = c.String("hostname")
		raven.SetTagsContext(tags)
		raven.CapturePanic(func() { err = tunnel.StartServer(c, version, shutdownC, graceShutdownC) }, nil)
		exitCode := 0
		if err != nil {
			handleError(err)
			exitCode = 1
		}
		// we already handle error printing, so we pass an empty string so we
		// don't have to print again.
		return cli.Exit("", exitCode)
	}
}

func userHomeDir() (string, error) {
	// This returns the home dir of the executing user using OS-specific method
	// for discovering the home dir. It's not recommended to call this function
	// when the user has root permission as $HOME depends on what options the user
	// use with sudo.
	homeDir, err := homedir.Dir()
	if err != nil {
		logger.WithError(err).Error("Cannot determine home directory for the user")
		return "", errors.Wrap(err, "Cannot determine home directory for the user")
	}
	return homeDir, nil
}

// In order to keep the amount of noise sent to Sentry low, typical network errors can be filtered out here by a substring match.
func handleError(err error) {
	errorMessage := err.Error()
	for _, ignoredErrorMessage := range ignoredErrors {
		if strings.Contains(errorMessage, ignoredErrorMessage) {
			return
		}
	}
	raven.CaptureError(err, nil)
}
