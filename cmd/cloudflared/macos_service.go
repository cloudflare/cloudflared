// +build darwin

package main

import (
	"fmt"
	"os"

	"gopkg.in/urfave/cli.v2"
)

const launchAgentIdentifier = "com.cloudflare.cloudflared"

func runApp(app *cli.App) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the Argo Tunnel launch agent",
		Subcommands: []*cli.Command{
			{
				Name:   "install",
				Usage:  "Install Argo Tunnel as an user launch agent",
				Action: installLaunchd,
			},
			{
				Name:   "uninstall",
				Usage:  "Uninstall the Argo Tunnel launch agent",
				Action: uninstallLaunchd,
			},
		},
	})
	app.Run(os.Args)
}

var launchdTemplate = ServiceTemplate{
	Path: fmt.Sprintf("~/Library/LaunchAgents/%s.plist", launchAgentIdentifier),
	Content: fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
	<dict>
		<key>Label</key>
		<string>%s</string>
		<key>Program</key>
		<string>{{ .Path }}</string>
		<key>RunAtLoad</key>
		<true/>
		<key>StandardOutPath</key>
		<string>/tmp/%s.out.log</string>
    <key>StandardErrorPath</key>
		<string>/tmp/%s.err.log</string>
		<key>KeepAlive</key>
		<dict>
			<key>SuccessfulExit</key>
			<false/>
		</dict>
		<key>ThrottleInterval</key>
		<integer>20</integer>
	</dict>
</plist>`, launchAgentIdentifier, launchAgentIdentifier, launchAgentIdentifier),
}

func installLaunchd(c *cli.Context) error {
	Log.Infof("Installing Argo Tunnel as an user launch agent")
	etPath, err := os.Executable()
	if err != nil {
		Log.WithError(err).Infof("error determining executable path")
		return fmt.Errorf("error determining executable path: %v", err)
	}
	templateArgs := ServiceTemplateArgs{Path: etPath}
	err = launchdTemplate.Generate(&templateArgs)
	if err != nil {
		Log.WithError(err).Infof("error generating launchd template")
		return err
	}
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		Log.WithError(err).Infof("error resolving launchd template path")
		return err
	}
	Log.Infof("Outputs are logged in %s and %s", fmt.Sprintf("/tmp/%s.out.log", launchAgentIdentifier), fmt.Sprintf("/tmp/%s.err.log", launchAgentIdentifier))
	return runCommand("launchctl", "load", plistPath)
}

func uninstallLaunchd(c *cli.Context) error {
	Log.Infof("Uninstalling Argo Tunnel as an user launch agent")
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		Log.WithError(err).Infof("error resolving launchd template path")
		return err
	}
	err = runCommand("launchctl", "unload", plistPath)
	if err != nil {
		Log.WithError(err).Infof("error unloading")
		return err
	}
	Log.Infof("Outputs are logged in %s and %s", fmt.Sprintf("/tmp/%s.out.log", launchAgentIdentifier), fmt.Sprintf("/tmp/%s.err.log", launchAgentIdentifier))
	return launchdTemplate.Remove()
}
