//go:build darwin

package main

import (
	"fmt"
	"os"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
)

const (
	launchdIdentifier = "com.cloudflare.cloudflared"
)

func runApp(app *cli.App, _ chan struct{}) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the cloudflared launch agent",
		Subcommands: []*cli.Command{
			{
				Name:   "install",
				Usage:  "Install cloudflared as an user launch agent",
				Action: cliutil.ConfiguredAction(installLaunchd),
			},
			{
				Name:   "uninstall",
				Usage:  "Uninstall the cloudflared launch agent",
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
			{{- range $i, $item := .ExtraArgs}}
			<string>{{ $item }}</string>
			{{- end}}
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
		log.Info().Msg("Installing cloudflared client as a system launch daemon. " +
			"cloudflared client will run at boot")
	} else {
		log.Info().Msg("Installing cloudflared client as an user launch agent. " +
			"Note that cloudflared client will only run when the user is logged in. " +
			"If you want to run cloudflared client at boot, install with root permission. " +
			"For more information, visit https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/configure-tunnels/local-management/as-a-service/macos/")
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
	extraArgs, err := getServiceExtraArgsFromCliArgs(c, log)
	if err != nil {
		errMsg := "Unable to determine extra arguments for launch daemon"
		log.Err(err).Msg(errMsg)
		return errors.Wrap(err, errMsg)
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
	templateArgs := ServiceTemplateArgs{Path: etPath, ExtraArgs: extraArgs}
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
	err = runCommand("launchctl", "load", plistPath)
	if err == nil {
		log.Info().Msg("MacOS service for cloudflared installed successfully")
	}
	return err
}

func uninstallLaunchd(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	if isRootUser() {
		log.Info().Msg("Uninstalling cloudflared as a system launch daemon")
	} else {
		log.Info().Msg("Uninstalling cloudflared as a user launch agent")
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
		log.Err(err).Msg("error unloading launchd")
		return err
	}

	err = launchdTemplate.Remove()
	if err == nil {
		log.Info().Msg("Launchd for cloudflared was uninstalled successfully")
	}
	return err
}

func userHomeDir() (string, error) {
	// This returns the home dir of the executing user using OS-specific method
	// for discovering the home dir. It's not recommended to call this function
	// when the user has root permission as $HOME depends on what options the user
	// use with sudo.
	homeDir, err := homedir.Dir()
	if err != nil {
		return "", errors.Wrap(err, "Cannot determine home directory for the user")
	}
	return homeDir, nil
}
