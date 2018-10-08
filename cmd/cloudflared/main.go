package main

import (
	"fmt"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/access"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/updater"
	"github.com/cloudflare/cloudflared/log"
	"github.com/cloudflare/cloudflared/metrics"

	"github.com/getsentry/raven-go"
	"github.com/mitchellh/go-homedir"
	"gopkg.in/urfave/cli.v2"

	"github.com/pkg/errors"
)

const (
	developerPortal = "https://developers.cloudflare.com/argo-tunnel"
	licenseUrl      = developerPortal + "/licence/"
)

var (
	Version   = "DEV"
	BuildTime = "unknown"
	logger    = log.CreateLogger()
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

	app := &cli.App{}
	app.Name = "cloudflared"
	app.Usage = "Cloudflare's command-line tool and agent"
	app.ArgsUsage = "origin-url"
	app.Copyright = fmt.Sprintf(`(c) %d Cloudflare Inc.
   Use is subject to the license agreement at %s`, time.Now().Year(), licenseUrl)
	app.Version = fmt.Sprintf("%s (built %s)", Version, BuildTime)
	app.Description = `cloudflared connects your machine or user identity to Cloudflare's global network. 
	You can use it to authenticate a session to reach an API behind Access, route web traffic to this machine,
	and configure access control.`
	app.Flags = flags()
	app.Action = action(Version, shutdownC, graceShutdownC)
	app.Before = tunnel.Before
	app.Commands = commands()

	tunnel.Init(Version, shutdownC, graceShutdownC) // we need this to support the tunnel sub command...
	runApp(app, shutdownC, graceShutdownC)
}

func commands() []*cli.Command {
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
	}
	cmds = append(cmds, tunnel.Commands()...)
	cmds = append(cmds, access.Commands()...)
	return cmds
}

func flags() []cli.Flag {
	flags := tunnel.Flags()
	return append(flags, access.Flags()...)
}

func action(version string, shutdownC, graceShutdownC chan struct{}) cli.ActionFunc {
	return func(c *cli.Context) (err error) {
		tags := make(map[string]string)
		tags["hostname"] = c.String("hostname")
		raven.SetTagsContext(tags)
		raven.CapturePanic(func() { err = tunnel.StartServer(c, version, shutdownC, graceShutdownC) }, nil)
		if err != nil {
			raven.CaptureError(err, nil)
		}
		return err
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
