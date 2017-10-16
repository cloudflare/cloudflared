// +build darwin

package main

import (
	"fmt"
	"os"

	cli "gopkg.in/urfave/cli.v2"
)

func runApp(app *cli.App) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the Cloudflare Warp launch agent",
		Subcommands: []*cli.Command{
			&cli.Command{
				Name:   "install",
				Usage:  "Install Cloudflare Warp as an user launch agent",
				Action: installLaunchd,
			},
			&cli.Command{
				Name:   "uninstall",
				Usage:  "Uninstall the Cloudflare Warp launch agent",
				Action: uninstallLaunchd,
			},
		},
	})
	app.Run(os.Args)
}

var launchdTemplate = ServiceTemplate{
	Path: "~/Library/LaunchAgents/com.cloudflare.warp.plist",
	Content: `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
	<dict>
		<key>Label</key>
		<string>com.cloudflare.warp</string>
		<key>Program</key>
		<string>{{ .Path }}</string>
		<key>RunAtLoad</key>
		<true/>
		<key>KeepAlive</key>
		<dict>
			<key>NetworkState</key>
			<true/>
		</dict>
		<key>ThrottleInterval</key>
		<integer>20</integer>
	</dict>
</plist>`,
}

func installLaunchd(c *cli.Context) error {
	etPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error determining executable path: %v", err)
	}
	templateArgs := ServiceTemplateArgs{Path: etPath}
	err = launchdTemplate.Generate(&templateArgs)
	if err != nil {
		return err
	}
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		return err
	}
	return runCommand("launchctl", "load", plistPath)
}

func uninstallLaunchd(c *cli.Context) error {
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		return err
	}
	err = runCommand("launchctl", "unload", plistPath)
	if err != nil {
		return err
	}
	return launchdTemplate.Remove()
}
