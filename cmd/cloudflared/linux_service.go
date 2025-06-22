//go:build linux

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
)

func runApp(app *cli.App, _ chan struct{}) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the cloudflared system service",
		Subcommands: []*cli.Command{
			{
				Name:   "install",
				Usage:  "Install cloudflared as a system service",
				Action: cliutil.ConfiguredAction(installLinuxService),
				Flags: []cli.Flag{
					noUpdateServiceFlag,
				},
			},
			{
				Name:   "uninstall",
				Usage:  "Uninstall the cloudflared service",
				Action: cliutil.ConfiguredAction(uninstallLinuxService),
			},
		},
	})
	_ = app.Run(os.Args)
}

// The directory and files that are used by the service.
// These are hard-coded in the templates below.
const (
	serviceConfigDir         = "/etc/cloudflared"
	serviceConfigFile        = "config.yml"
	serviceCredentialFile    = "cert.pem"
	serviceConfigPath        = serviceConfigDir + "/" + serviceConfigFile
	cloudflaredService       = "cloudflared.service"
	cloudflaredUpdateService = "cloudflared-update.service"
	cloudflaredUpdateTimer   = "cloudflared-update.timer"
)

var systemdAllTemplates = map[string]ServiceTemplate{
	cloudflaredService: {
		Path: fmt.Sprintf("/etc/systemd/system/%s", cloudflaredService),
		Content: `[Unit]
Description=cloudflared
After=network-online.target
Wants=network-online.target

[Service]
TimeoutStartSec=0
Type=notify
ExecStart={{ .Path }} --no-autoupdate{{ range .ExtraArgs }} {{ . }}{{ end }}
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
`,
	},
	cloudflaredUpdateService: {
		Path: fmt.Sprintf("/etc/systemd/system/%s", cloudflaredUpdateService),
		Content: `[Unit]
Description=Update cloudflared
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/bin/bash -c '{{ .Path }} update; code=$?; if [ $code -eq 11 ]; then systemctl restart cloudflared; exit 0; fi; exit $code'
`,
	},
	cloudflaredUpdateTimer: {
		Path: fmt.Sprintf("/etc/systemd/system/%s", cloudflaredUpdateTimer),
		Content: `[Unit]
Description=Update cloudflared

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
	// nolint: dupword
	Content: `#!/bin/sh
# For RedHat and cousins:
# chkconfig: 2345 99 01
# description: cloudflared
# processname: {{.Path}}
### BEGIN INIT INFO
# Provides:          {{.Path}}
# Required-Start:
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: cloudflared
# Description:       cloudflared agent
### END INIT INFO
name=$(basename $(readlink -f $0))
cmd="{{.Path}} --pidfile /var/run/$name.pid {{ range .ExtraArgs }} {{ . }}{{ end }}"
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

var noUpdateServiceFlag = &cli.BoolFlag{
	Name:  "no-update-service",
	Usage: "Disable auto-update of the cloudflared linux service, which restarts the server to upgrade for new versions.",
	Value: false,
}

func isSystemd() bool {
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return true
	}
	return false
}

func installLinuxService(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	etPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error determining executable path: %v", err)
	}
	templateArgs := ServiceTemplateArgs{
		Path: etPath,
	}

	// Check if the "no update flag" is set
	autoUpdate := !c.IsSet(noUpdateServiceFlag.Name)

	var extraArgsFunc func(c *cli.Context, log *zerolog.Logger) ([]string, error)
	if c.NArg() == 0 {
		extraArgsFunc = buildArgsForConfig
	} else {
		extraArgsFunc = buildArgsForToken
	}

	extraArgs, err := extraArgsFunc(c, log)
	if err != nil {
		return err
	}

	templateArgs.ExtraArgs = extraArgs

	switch {
	case isSystemd():
		log.Info().Msgf("Using Systemd")
		err = installSystemd(&templateArgs, autoUpdate, log)
	default:
		log.Info().Msgf("Using SysV")
		err = installSysv(&templateArgs, autoUpdate, log)
	}

	if err == nil {
		log.Info().Msg("Linux service for cloudflared installed successfully")
	}
	return err
}

func buildArgsForConfig(c *cli.Context, log *zerolog.Logger) ([]string, error) {
	if err := ensureConfigDirExists(serviceConfigDir); err != nil {
		return nil, err
	}

	src, _, err := config.ReadConfigFile(c, log)
	if err != nil {
		return nil, err
	}

	// can't use context because this command doesn't define "credentials-file" flag
	configPresent := func(s string) bool {
		val, err := src.String(s)
		return err == nil && val != ""
	}
	if src.TunnelID == "" || !configPresent(tunnel.CredFileFlag) {
		return nil, fmt.Errorf(`Configuration file %s must contain entries for the tunnel to run and its associated credentials:
tunnel: TUNNEL-UUID
credentials-file: CREDENTIALS-FILE
`, src.Source())
	}
	if src.Source() != serviceConfigPath {
		if exists, err := config.FileExists(serviceConfigPath); err != nil || exists {
			return nil, fmt.Errorf("Possible conflicting configuration in %[1]s and %[2]s. Either remove %[2]s or run `cloudflared --config %[2]s service install`", src.Source(), serviceConfigPath)
		}

		if err := copyFile(src.Source(), serviceConfigPath); err != nil {
			return nil, fmt.Errorf("failed to copy %s to %s: %w", src.Source(), serviceConfigPath, err)
		}
	}

	return []string{
		"--config", "/etc/cloudflared/config.yml", "tunnel", "run",
	}, nil
}

func installSystemd(templateArgs *ServiceTemplateArgs, autoUpdate bool, log *zerolog.Logger) error {
	var systemdTemplates []ServiceTemplate
	if autoUpdate {
		systemdTemplates = []ServiceTemplate{
			systemdAllTemplates[cloudflaredService],
			systemdAllTemplates[cloudflaredUpdateService],
			systemdAllTemplates[cloudflaredUpdateTimer],
		}
	} else {
		systemdTemplates = []ServiceTemplate{
			systemdAllTemplates[cloudflaredService],
		}
	}

	for _, serviceTemplate := range systemdTemplates {
		err := serviceTemplate.Generate(templateArgs)
		if err != nil {
			log.Err(err).Msg("error generating service template")
			return err
		}
	}
	if err := runCommand("systemctl", "enable", cloudflaredService); err != nil {
		log.Err(err).Msgf("systemctl enable %s error", cloudflaredService)
		return err
	}

	if autoUpdate {
		if err := runCommand("systemctl", "start", cloudflaredUpdateTimer); err != nil {
			log.Err(err).Msgf("systemctl start %s error", cloudflaredUpdateTimer)
			return err
		}
	}

	if err := runCommand("systemctl", "daemon-reload"); err != nil {
		log.Err(err).Msg("systemctl daemon-reload error")
		return err
	}
	return runCommand("systemctl", "start", cloudflaredService)
}

func installSysv(templateArgs *ServiceTemplateArgs, autoUpdate bool, log *zerolog.Logger) error {
	confPath, err := sysvTemplate.ResolvePath()
	if err != nil {
		log.Err(err).Msg("error resolving system path")
		return err
	}

	if autoUpdate {
		templateArgs.ExtraArgs = append([]string{"--autoupdate-freq 24h0m0s"}, templateArgs.ExtraArgs...)
	} else {
		templateArgs.ExtraArgs = append([]string{"--no-autoupdate"}, templateArgs.ExtraArgs...)
	}

	if err := sysvTemplate.Generate(templateArgs); err != nil {
		log.Err(err).Msg("error generating system template")
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
	return runCommand("service", "cloudflared", "start")
}

func uninstallLinuxService(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	var err error
	switch {
	case isSystemd():
		log.Info().Msg("Using Systemd")
		err = uninstallSystemd(log)
	default:
		log.Info().Msg("Using SysV")
		err = uninstallSysv(log)
	}

	if err == nil {
		log.Info().Msg("Linux service for cloudflared uninstalled successfully")
	}
	return err
}

func uninstallSystemd(log *zerolog.Logger) error {
	// Get only the installed services
	installedServices := make(map[string]ServiceTemplate)
	for serviceName, serviceTemplate := range systemdAllTemplates {
		if err := runCommand("systemctl", "list-units", "--all", "|", "grep", serviceName); err == nil {
			installedServices[serviceName] = serviceTemplate
		} else {
			log.Info().Msgf("Service '%s' not installed, skipping its uninstall", serviceName)
		}
	}

	if _, exists := installedServices[cloudflaredService]; exists {
		if err := runCommand("systemctl", "disable", cloudflaredService); err != nil {
			log.Err(err).Msgf("systemctl disable %s error", cloudflaredService)
			return err
		}
		if err := runCommand("systemctl", "stop", cloudflaredService); err != nil {
			log.Err(err).Msgf("systemctl stop %s error", cloudflaredService)
			return err
		}
	}

	if _, exists := installedServices[cloudflaredUpdateTimer]; exists {
		if err := runCommand("systemctl", "stop", cloudflaredUpdateTimer); err != nil {
			log.Err(err).Msgf("systemctl stop %s error", cloudflaredUpdateTimer)
			return err
		}
	}

	for _, serviceTemplate := range installedServices {
		if err := serviceTemplate.Remove(); err != nil {
			log.Err(err).Msg("error removing service template")
			return err
		}
	}
	if err := runCommand("systemctl", "daemon-reload"); err != nil {
		log.Err(err).Msg("systemctl daemon-reload error")
		return err
	}
	return nil
}

func uninstallSysv(log *zerolog.Logger) error {
	if err := runCommand("service", "cloudflared", "stop"); err != nil {
		log.Err(err).Msg("service cloudflared stop error")
		return err
	}
	if err := sysvTemplate.Remove(); err != nil {
		log.Err(err).Msg("error removing service template")
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
	return nil
}

func ensureConfigDirExists(configDir string) error {
	ok, err := config.FileExists(configDir)
	if !ok && err == nil {
		err = os.Mkdir(configDir, 0755)
	}
	return err
}

func copyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		destFile.Close()
		if !ok {
			_ = os.Remove(dest)
		}
	}()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return err
	}

	ok = true
	return nil
}
