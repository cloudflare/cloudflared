// +build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	cli "github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
)

func runApp(app *cli.App, shutdownC, graceShutdownC chan struct{}) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the Argo Tunnel system service",
		Subcommands: []*cli.Command{
			&cli.Command{
				Name:   "install",
				Usage:  "Install Argo Tunnel as a system service",
				Action: cliutil.ErrorHandler(installLinuxService),
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "legacy",
						Usage: "Generate service file for non-named tunnels",
					},
				},
			},
			&cli.Command{
				Name:   "uninstall",
				Usage:  "Uninstall the Argo Tunnel service",
				Action: cliutil.ErrorHandler(uninstallLinuxService),
			},
		},
	})
	app.Run(os.Args)
}

// The directory and files that are used by the service.
// These are hard-coded in the templates below.
const (
	serviceConfigDir      = "/etc/cloudflared"
	serviceConfigFile     = "config.yml"
	serviceCredentialFile = "cert.pem"
	serviceConfigPath     = serviceConfigDir + "/" + serviceConfigFile
)

var systemdTemplates = []ServiceTemplate{
	{
		Path: "/etc/systemd/system/cloudflared.service",
		Content: `[Unit]
Description=Argo Tunnel
After=network.target

[Service]
TimeoutStartSec=0
Type=notify
ExecStart={{ .Path }} --config /etc/cloudflared/config.yml --no-autoupdate{{ range .ExtraArgs }} {{ . }}{{ end }}
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
`,
	},
	{
		Path: "/etc/systemd/system/cloudflared-update.service",
		Content: `[Unit]
Description=Update Argo Tunnel
After=network.target

[Service]
ExecStart=/bin/bash -c '{{ .Path }} update; code=$?; if [ $code -eq 11 ]; then systemctl restart cloudflared; exit 0; fi; exit $code'
`,
	},
	{
		Path: "/etc/systemd/system/cloudflared-update.timer",
		Content: `[Unit]
Description=Update Argo Tunnel

[Timer]
OnCalendar=daily

[Install]
WantedBy=timers.target
`,
	},
}

var sysvTemplate = ServiceTemplate{
	Path:     "/etc/init.d/cloudflared",
	FileMode: 0755,
	Content: `#!/bin/sh
# For RedHat and cousins:
# chkconfig: 2345 99 01
# description: Argo Tunnel agent
# processname: {{.Path}}
### BEGIN INIT INFO
# Provides:          {{.Path}}
# Required-Start:
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: Argo Tunnel
# Description:       Argo Tunnel agent
### END INIT INFO
name=$(basename $(readlink -f $0))
cmd="{{.Path}} --config /etc/cloudflared/config.yml --pidfile /var/run/$name.pid --autoupdate-freq 24h0m0s{{ range .ExtraArgs }} {{ . }}{{ end }}"
pid_file="/var/run/$name.pid"
stdout_log="/var/log/$name.log"
stderr_log="/var/log/$name.err"
[ -e /etc/sysconfig/$name ] && . /etc/sysconfig/$name
get_pid() {
    cat "$pid_file"
}
is_running() {
    [ -f "$pid_file" ] && ps $(get_pid) > /dev/null 2>&1
}
case "$1" in
    start)
        if is_running; then
            echo "Already started"
        else
            echo "Starting $name"
            $cmd >> "$stdout_log" 2>> "$stderr_log" &
            echo $! > "$pid_file"
        fi
    ;;
    stop)
        if is_running; then
            echo -n "Stopping $name.."
            kill $(get_pid)
            for i in {1..10}
            do
                if ! is_running; then
                    break
                fi
                echo -n "."
                sleep 1
            done
            echo
            if is_running; then
                echo "Not stopped; may still be shutting down or shutdown may have failed"
                exit 1
            else
                echo "Stopped"
                if [ -f "$pid_file" ]; then
                    rm "$pid_file"
                fi
            fi
        else
            echo "Not running"
        fi
    ;;
    restart)
        $0 stop
        if is_running; then
            echo "Unable to stop, will not attempt to start"
            exit 1
        fi
        $0 start
    ;;
    status)
        if is_running; then
            echo "Running"
        else
            echo "Stopped"
            exit 1
        fi
    ;;
    *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
    ;;
esac
exit 0
`,
}

func isSystemd() bool {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return true
	}
	return false
}

func copyUserConfiguration(userConfigDir, userConfigFile, userCredentialFile string, logger logger.Service) error {
	srcCredentialPath := filepath.Join(userConfigDir, userCredentialFile)
	destCredentialPath := filepath.Join(serviceConfigDir, serviceCredentialFile)
	if srcCredentialPath != destCredentialPath {
		if err := copyCredential(srcCredentialPath, destCredentialPath); err != nil {
			return err
		}
	}

	srcConfigPath := filepath.Join(userConfigDir, userConfigFile)
	destConfigPath := filepath.Join(serviceConfigDir, serviceConfigFile)
	if srcConfigPath != destConfigPath {
		if err := copyConfig(srcConfigPath, destConfigPath); err != nil {
			return err
		}
		logger.Infof("Copied %s to %s", srcConfigPath, destConfigPath)
	}

	return nil
}

func installLinuxService(c *cli.Context) error {
	logger, err := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	etPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error determining executable path: %v", err)
	}
	templateArgs := ServiceTemplateArgs{
		Path: etPath,
	}

	if err := ensureConfigDirExists(serviceConfigDir); err != nil {
		return err
	}
	if c.Bool("legacy") {
		userConfigDir := filepath.Dir(c.String("config"))
		userConfigFile := filepath.Base(c.String("config"))
		userCredentialFile := config.DefaultCredentialFile
		if err = copyUserConfiguration(userConfigDir, userConfigFile, userCredentialFile, logger); err != nil {
			logger.Errorf("Failed to copy user configuration: %s. Before running the service, ensure that %s contains two files, %s and %s", err,
				serviceConfigDir, serviceCredentialFile, serviceConfigFile)
			return err
		}
		templateArgs.ExtraArgs = []string{
			"--origincert", serviceConfigDir + "/" + serviceCredentialFile,
		}
	} else {
		src, err := config.ReadConfigFile(c, logger)
		if err != nil {
			return err
		}

		// can't use context because this command doesn't define "credentials-file" flag
		configPresent := func(s string) bool {
			val, err := src.String(s)
			return err == nil && val != ""
		}
		if src.TunnelID == "" || !configPresent("credentials-file") {
			return fmt.Errorf(`Configuration file %s must contain entries for the tunnel to run and its associated credentials:
tunnel: TUNNEL-UUID
credentials-file: CREDENTIALS-FILE
`, src.Source())
		}
		if src.Source() != serviceConfigPath {
			if exists, err := config.FileExists(serviceConfigPath); err != nil || exists {
				return fmt.Errorf("Possible conflicting configuration in %[1]s and %[2]s. Either remove %[2]s or run `cloudflared --config %[2]s service install`", src.Source(), serviceConfigPath)
			}

			if err := copyFile(src.Source(), serviceConfigPath); err != nil {
				return fmt.Errorf("failed to copy %s to %s: %w", src.Source(), serviceConfigPath, err)
			}
		}

		templateArgs.ExtraArgs = []string{
			"tunnel", "run",
		}
	}

	switch {
	case isSystemd():
		logger.Infof("Using Systemd")
		return installSystemd(&templateArgs, logger)
	default:
		logger.Infof("Using SysV")
		return installSysv(&templateArgs, logger)
	}
}

func installSystemd(templateArgs *ServiceTemplateArgs, logger logger.Service) error {
	for _, serviceTemplate := range systemdTemplates {
		err := serviceTemplate.Generate(templateArgs)
		if err != nil {
			logger.Errorf("error generating service template: %s", err)
			return err
		}
	}
	if err := runCommand("systemctl", "enable", "cloudflared.service"); err != nil {
		logger.Errorf("systemctl enable cloudflared.service error: %s", err)
		return err
	}
	if err := runCommand("systemctl", "start", "cloudflared-update.timer"); err != nil {
		logger.Errorf("systemctl start cloudflared-update.timer error: %s", err)
		return err
	}
	logger.Infof("systemctl daemon-reload")
	return runCommand("systemctl", "daemon-reload")
}

func installSysv(templateArgs *ServiceTemplateArgs, logger logger.Service) error {
	confPath, err := sysvTemplate.ResolvePath()
	if err != nil {
		logger.Errorf("error resolving system path: %s", err)
		return err
	}
	if err := sysvTemplate.Generate(templateArgs); err != nil {
		logger.Errorf("error generating system template: %s", err)
		return err
	}
	for _, i := range [...]string{"2", "3", "4", "5"} {
		if err := os.Symlink(confPath, "/etc/rc"+i+".d/S50et"); err != nil {
			continue
		}
	}
	for _, i := range [...]string{"0", "1", "6"} {
		if err := os.Symlink(confPath, "/etc/rc"+i+".d/K02et"); err != nil {
			continue
		}
	}
	return nil
}

func uninstallLinuxService(c *cli.Context) error {
	logger, err := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	switch {
	case isSystemd():
		logger.Infof("Using Systemd")
		return uninstallSystemd(logger)
	default:
		logger.Infof("Using SysV")
		return uninstallSysv(logger)
	}
}

func uninstallSystemd(logger logger.Service) error {
	if err := runCommand("systemctl", "disable", "cloudflared.service"); err != nil {
		logger.Errorf("systemctl disable cloudflared.service error: %s", err)
		return err
	}
	if err := runCommand("systemctl", "stop", "cloudflared-update.timer"); err != nil {
		logger.Errorf("systemctl stop cloudflared-update.timer error: %s", err)
		return err
	}
	for _, serviceTemplate := range systemdTemplates {
		if err := serviceTemplate.Remove(); err != nil {
			logger.Errorf("error removing service template: %s", err)
			return err
		}
	}
	logger.Infof("Successfully uninstall cloudflared service")
	return nil
}

func uninstallSysv(logger logger.Service) error {
	if err := sysvTemplate.Remove(); err != nil {
		logger.Errorf("error removing service template: %s", err)
		return err
	}
	for _, i := range [...]string{"2", "3", "4", "5"} {
		if err := os.Remove("/etc/rc" + i + ".d/S50et"); err != nil {
			continue
		}
	}
	for _, i := range [...]string{"0", "1", "6"} {
		if err := os.Remove("/etc/rc" + i + ".d/K02et"); err != nil {
			continue
		}
	}
	logger.Infof("Successfully uninstall cloudflared service")
	return nil
}
