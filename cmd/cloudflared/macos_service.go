// +build darwin

package main

import (
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
)

const (
	launchdIdentifier = "com.cloudflare.cloudflared"
)

func runApp(app *cli.App, graceShutdownC chan struct{}) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the Argo Tunnel launch agent",
		Subcommands: []*cli.Command{
			{
				Name:   "install",
				Usage:  "Install Argo Tunnel as an user launch agent",
				Action: cliutil.ConfiguredAction(installLaunchd),
			},
			{
				Name:   "uninstall",
				Usage:  "Uninstall the Argo Tunnel launch agent",
				Action: cliutil.ConfiguredAction(uninstallLaunchd),
			},
		},
	})
	_ = app.Run(os.Args)
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
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	if isRootUser() {
		log.Info().Msg("Installing Argo Tunnel client as a system launch daemon. " +
			"Argo Tunnel client will run at boot")
	} else {
		log.Info().Msg("Installing Argo Tunnel client as an user launch agent. " +
			"Note that Argo Tunnel client will only run when the user is logged in. " +
			"If you want to run Argo Tunnel client at boot, install with root permission. " +
			"For more information, visit https://developers.cloudflare.com/argo-tunnel/reference/service/")
	}
	etPath, err := os.Executable()
	if err != nil {
		log.Err(err).Msg("Error determining executable path")
		return fmt.Errorf("Error determining executable path: %v", err)
	}
	installPath, err := installPath()
	if err != nil {
		log.Err(err).Msg("Error determining install path")
		return errors.Wrap(err, "Error determining install path")
	}
	stdoutPath, err := stdoutPath()
	if err != nil {
		log.Err(err).Msg("error determining stdout path")
		return errors.Wrap(err, "error determining stdout path")
	}
	stderrPath, err := stderrPath()
	if err != nil {
		log.Err(err).Msg("error determining stderr path")
		return errors.Wrap(err, "error determining stderr path")
	}
	launchdTemplate := newLaunchdTemplate(installPath, stdoutPath, stderrPath)
	templateArgs := ServiceTemplateArgs{Path: etPath}
	err = launchdTemplate.Generate(&templateArgs)
	if err != nil {
		log.Err(err).Msg("error generating launchd template")
		return err
	}
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		log.Err(err).Msg("error resolving launchd template path")
		return err
	}

	log.Info().Msgf("Outputs are logged to %s and %s", stderrPath, stdoutPath)
	return runCommand("launchctl", "load", plistPath)
}

func uninstallLaunchd(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	if isRootUser() {
		log.Info().Msg("Uninstalling Argo Tunnel as a system launch daemon")
	} else {
		log.Info().Msg("Uninstalling Argo Tunnel as an user launch agent")
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
	plistPath, err := launchdTemplate.ResolvePath()
	if err != nil {
		log.Err(err).Msg("error resolving launchd template path")
		return err
	}
	err = runCommand("launchctl", "unload", plistPath)
	if err != nil {
		log.Err(err).Msg("error unloading")
		return err
	}

	log.Info().Msgf("Outputs are logged to %s and %s", stderrPath, stdoutPath)
	return launchdTemplate.Remove()
}
