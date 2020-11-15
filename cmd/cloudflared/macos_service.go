// +build darwin

package main

import (
	"fmt"
	"os"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
)

const (
	launchdIdentifier = "com.cloudflare.cloudflared"
)

func runApp(app *cli.App, shutdownC, graceShutdownC chan struct{}) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the Argo Tunnel launch agent",
		Subcommands: []*cli.Command{
			{
				Name:   "install",
				Usage:  "Install Argo Tunnel as an user launch agent",
				Action: cliutil.ErrorHandler(installLaunchd),
			},
			{
				Name:   "uninstall",
				Usage:  "Uninstall the Argo Tunnel launch agent",
				Action: cliutil.ErrorHandler(uninstallLaunchd),
			},
		},
	})
	app.Run(os.Args)
}

func newLaunchdTemplate(installPath, stdoutPath, stderrPath string) *ServiceTemplate {
	return &ServiceTemplate{
		Path: installPath,
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
		<integer>5</integer>
	</dict>
</plist>`, launchdIdentifier, stdoutPath, stderrPath),
	}
}

func isRootUser() bool {
	return os.Geteuid() == 0
}

func installPath() (string, error) {
	// User is root, use /Library/LaunchDaemons instead of home directory
	if isRootUser() {
		return fmt.Sprintf("/Library/LaunchDaemons/%s.plist", launchdIdentifier), nil
	}
	userHomeDir, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/Library/LaunchAgents/%s.plist", userHomeDir, launchdIdentifier), nil
}

func stdoutPath() (string, error) {
	if isRootUser() {
		return fmt.Sprintf("/Library/Logs/%s.out.log", launchdIdentifier), nil
	}
	userHomeDir, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/Library/Logs/%s.out.log", userHomeDir, launchdIdentifier), nil
}

func stderrPath() (string, error) {
	if isRootUser() {
		return fmt.Sprintf("/Library/Logs/%s.err.log", launchdIdentifier), nil
	}
	userHomeDir, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/Library/Logs/%s.err.log", userHomeDir, launchdIdentifier), nil
}

func installLaunchd(c *cli.Context) error {
	logger, err := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

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
		logger.Errorf("Error determining executable path: %s", err)
		return fmt.Errorf("Error determining executable path: %v", err)
	}
	installPath, err := installPath()
	if err != nil {
		logger.Errorf("Error determining install path: %s", err)
		return errors.Wrap(err, "Error determining install path")
	}
	stdoutPath, err := stdoutPath()
	if err != nil {
		logger.Errorf("error determining stdout path: %s", err)
		return errors.Wrap(err, "error determining stdout path")
	}
	stderrPath, err := stderrPath()
	if err != nil {
		logger.Errorf("error determining stderr path: %s", err)
		return errors.Wrap(err, "error determining stderr path")
	}
	launchdTemplate := newLaunchdTemplate(installPath, stdoutPath, stderrPath)
	if err != nil {
		logger.Errorf("error creating launchd template: %s", err)
		return errors.Wrap(err, "error creating launchd template")
	}
	templateArgs := ServiceTemplateArgs{Path: etPath}
	err = launchdTemplate.Generate(&templateArgs)
	if err != nil {
		logger.Errorf("error generating launchd template: %s", err)
		return err
	}
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		logger.Errorf("error resolving launchd template path: %s", err)
		return err
	}

	logger.Infof("Outputs are logged to %s and %s", stderrPath, stdoutPath)
	return runCommand("launchctl", "load", plistPath)
}

func uninstallLaunchd(c *cli.Context) error {
	logger, err := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	if isRootUser() {
		logger.Infof("Uninstalling Argo Tunnel as a system launch daemon")
	} else {
		logger.Infof("Uninstalling Argo Tunnel as an user launch agent")
	}
	installPath, err := installPath()
	if err != nil {
		return errors.Wrap(err, "error determining install path")
	}
	stdoutPath, err := stdoutPath()
	if err != nil {
		return errors.Wrap(err, "error determining stdout path")
	}
	stderrPath, err := stderrPath()
	if err != nil {
		return errors.Wrap(err, "error determining stderr path")
	}
	launchdTemplate := newLaunchdTemplate(installPath, stdoutPath, stderrPath)
	if err != nil {
		return errors.Wrap(err, "error creating launchd template")
	}
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		logger.Errorf("error resolving launchd template path: %s", err)
		return err
	}
	err = runCommand("launchctl", "unload", plistPath)
	if err != nil {
		logger.Errorf("error unloading: %s", err)
		return err
	}

	logger.Infof("Outputs are logged to %s and %s", stderrPath, stdoutPath)
	return launchdTemplate.Remove()
}
