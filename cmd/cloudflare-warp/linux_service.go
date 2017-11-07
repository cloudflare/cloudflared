// +build linux

package main

import (
	"fmt"
	"os"

	cli "gopkg.in/urfave/cli.v2"
)

func runApp(app *cli.App) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the Cloudflare Warp system service",
		Subcommands: []*cli.Command{
			&cli.Command{
				Name:   "install",
				Usage:  "Install Cloudflare Warp as a system service",
				Action: installLinuxService,
			},
			&cli.Command{
				Name:   "uninstall",
				Usage:  "Uninstall the Cloudflare Warp service",
				Action: uninstallLinuxService,
			},
		},
	})
	app.Run(os.Args)
}

const serviceConfigDir = "/etc/cloudflare-warp"

var systemdTemplates = []ServiceTemplate{
	{
		Path: "/etc/systemd/system/cloudflare-warp.service",
		Content: `[Unit]
Description=Cloudflare Warp
After=network.target

[Service]
TimeoutStartSec=0
Type=notify
ExecStart={{ .Path }} --config /etc/cloudflare-warp/config.yml --origincert /etc/cloudflare-warp/cert.pem --autoupdate 0s

[Install]
WantedBy=multi-user.target
`,
	},
	{
		Path: "/etc/systemd/system/cloudflare-warp-update.service",
		Content: `[Unit]
Description=Update Cloudflare Warp
After=network.target

[Service]
ExecStart=/bin/bash -c '{{ .Path }} update; code=$?; if [ $code -eq 64 ]; then systemctl restart cloudflare-warp; exit 0; fi; exit $code'
`,
	},
	{
		Path: "/etc/systemd/system/cloudflare-warp-update.timer",
		Content: `[Unit]
Description=Update Cloudflare Warp

[Timer]
OnUnitActiveSec=1d 

[Install]
WantedBy=timers.target
`,
	},
}

var sysvTemplate = ServiceTemplate{
	Path:     "/etc/init.d/cloudflare-warp",
	FileMode: 0755,
	Content: `# For RedHat and cousins:
# chkconfig: 2345 99 01
# description: Cloudflare Warp agent
# processname: {{.Path}}
### BEGIN INIT INFO
# Provides:          {{.Path}}
# Required-Start:
# Required-Stop:
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: Cloudflare Warp
# Description:       Cloudflare Warp agent
### END INIT INFO
cmd="{{.Path}} --config /etc/cloudflare-warp/config.yml --origincert /etc/cloudflare-warp/cert.pem --pidfile /var/run/$name.pid"
name=$(basename $(readlink -f $0))
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
            if ! is_running; then
                echo "Unable to start, see $stdout_log and $stderr_log"
                exit 1
            fi
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

func installLinuxService(c *cli.Context) error {
	etPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error determining executable path: %v", err)
	}
	templateArgs := ServiceTemplateArgs{Path: etPath}

	if err = copyCredentials(serviceConfigDir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to copy user configuration: %v\n", err)
		fmt.Fprintf(os.Stderr, "Before running the service, ensure that %s contains two files, %s and %s",
			serviceConfigDir, credentialFile, configFile)
	}

	switch {
	case isSystemd():
		return installSystemd(&templateArgs)
	default:
		return installSysv(&templateArgs)
	}
}

func installSystemd(templateArgs *ServiceTemplateArgs) error {
	for _, serviceTemplate := range systemdTemplates {
		err := serviceTemplate.Generate(templateArgs)
		if err != nil {
			return err
		}
	}
	if err := runCommand("systemctl", "enable", "cloudflare-warp.service"); err != nil {
		return err
	}
	if err := runCommand("systemctl", "start", "cloudflare-warp-update.timer"); err != nil {
		return err
	}
	return runCommand("systemctl", "daemon-reload")
}

func installSysv(templateArgs *ServiceTemplateArgs) error {
	confPath, err := sysvTemplate.ResolvePath()
	if err != nil {
		return err
	}
	if err := sysvTemplate.Generate(templateArgs); err != nil {
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
	switch {
	case isSystemd():
		return uninstallSystemd()
	default:
		return uninstallSysv()
	}
}

func uninstallSystemd() error {
	if err := runCommand("systemctl", "disable", "cloudflare-warp.service"); err != nil {
		return err
	}
	if err := runCommand("systemctl", "stop", "cloudflare-warp-update.timer"); err != nil {
		return err
	}
	for _, serviceTemplate := range systemdTemplates {
		if err := serviceTemplate.Remove(); err != nil {
			return err
		}
	}
	return nil
}

func uninstallSysv() error {
	if err := sysvTemplate.Remove(); err != nil {
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
