// +build darwin

package main

import (
	"fmt"
	"os"

	"gopkg.in/urfave/cli.v2"
)

const (
	launchdIdentifier = "com.cloudflare.cloudflared"
)

func runApp(app *cli.App, shutdownC chan struct{}) {
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
	Path: installPath(),
	Content: fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
	<dict>
		<key>Label</key>
		<string>%s</string>
		<key>ProgramArguments</key>
		<array>
			<string>{{ .Path }}</string>
		</array>
		<key>RunAtLoad</key>
		<true/>
		<key>StandardOutPath</key>
		<string>%s</string>
		<key>StandardErrorPath</key>
		<string>%s</string>
		<key>KeepAlive</key>
		<dict>
			<key>SuccessfulExit</key>
			<false/>
		</dict>
		<key>ThrottleInterval</key>
		<integer>20</integer>
	</dict>
</plist>`, launchdIdentifier, stdoutPath(), stderrPath()),
}

func isRootUser() bool {
	return os.Geteuid() == 0
}

func installPath() string {
	// User is root, use /Library/LaunchDaemons instead of home directory
	if isRootUser() {
		return fmt.Sprintf("/Library/LaunchDaemons/%s.plist", launchdIdentifier)
	}
	return fmt.Sprintf("%s/Library/LaunchAgents/%s.plist", userHomeDir(), launchdIdentifier)
}

func stdoutPath() string {
	if isRootUser() {
		return fmt.Sprintf("/Library/Logs/%s.out.log", launchdIdentifier)
	}
	return fmt.Sprintf("%s/Library/Logs/%s.out.log", userHomeDir(), launchdIdentifier)
}

func stderrPath() string {
	if isRootUser() {
		return fmt.Sprintf("/Library/Logs/%s.err.log", launchdIdentifier)
	}
	return fmt.Sprintf("%s/Library/Logs/%s.err.log", userHomeDir(), launchdIdentifier)
}

func installLaunchd(c *cli.Context) error {
	if isRootUser() {
		logger.Infof("Installing Argo Tunnel client as a system launch daemon. " +
			"Argo Tunnel client will run at boot")
	} else {
		logger.Infof("Installing Argo Tunnel client as an user launch agent. " +
			"Note that Argo Tunnel client will only run when the user is logged in. " +
			"If you want to run Argo Tunnel client at boot, install with root permission. " +
			"For more information, visit https://developers.cloudflare.com/argo-tunnel/reference/service/")
	}
	etPath, err := os.Executable()
	if err != nil {
		logger.WithError(err).Infof("error determining executable path")
		return fmt.Errorf("error determining executable path: %v", err)
	}
	templateArgs := ServiceTemplateArgs{Path: etPath}
	err = launchdTemplate.Generate(&templateArgs)
	if err != nil {
		logger.WithError(err).Infof("error generating launchd template")
		return err
	}
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		logger.WithError(err).Infof("error resolving launchd template path")
		return err
	}

	logger.Infof("Outputs are logged to %s and %s", stderrPath(), stdoutPath())
	return runCommand("launchctl", "load", plistPath)
}

func uninstallLaunchd(c *cli.Context) error {
	if isRootUser() {
		logger.Infof("Uninstalling Argo Tunnel as a system launch daemon")
	} else {
		logger.Infof("Uninstalling Argo Tunnel as an user launch agent")
	}
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		logger.WithError(err).Infof("error resolving launchd template path")
		return err
	}
	err = runCommand("launchctl", "unload", plistPath)
	if err != nil {
		logger.WithError(err).Infof("error unloading")
		return err
	}

	logger.Infof("Outputs are logged to %s and %s", stderrPath(), stdoutPath())
	return launchdTemplate.Remove()
}
