// +build windows

package main

// Copypasta from the example files:
// https://github.com/golang/sys/blob/master/windows/svc/example

import (
	"fmt"
	"os"

	log "github.com/Sirupsen/logrus"
	cli "gopkg.in/urfave/cli.v2"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceName        = "CloudflareWarp"
	windowsServiceDescription = "Cloudflare Warp agent"
)

func runApp(app *cli.App) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the Cloudflare Warp Windows service",
		Subcommands: []*cli.Command{
			&cli.Command{
				Name:   "install",
				Usage:  "Install Cloudflare Warp as a Windows service",
				Action: installWindowsService,
			},
			&cli.Command{
				Name:   "uninstall",
				Usage:  "Uninstall the Cloudflare Warp service",
				Action: uninstallWindowsService,
			},
		},
	})

	isIntSess, err := svc.IsAnInteractiveSession()
	if err != nil {
		log.Fatalf("failed to determine if we are running in an interactive session: %v", err)
	}

	if isIntSess {
		app.Run(os.Args)
		return
	}

	elog, err := eventlog.Open(windowsServiceName)
	if err != nil {
		return
	}
	defer elog.Close()

	elog.Info(1, fmt.Sprintf("%s service starting", windowsServiceName))
	err = svc.Run(windowsServiceName, &windowsService{app: app, elog: elog})
	if err != nil {
		elog.Error(1, fmt.Sprintf("%s service failed: %v", windowsServiceName, err))
		return
	}
	elog.Info(1, fmt.Sprintf("%s service stopped", windowsServiceName))
}

type windowsService struct {
	app  *cli.App
	elog *eventlog.Log
}

func (s *windowsService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	go s.app.Run(args)
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				break loop
			default:
				s.elog.Error(1, fmt.Sprintf("unexpected control request #%d", c))
			}
		}
	}
	close(shutdownC)
	changes <- svc.Status{State: svc.StopPending}
	return
}

func installWindowsService(c *cli.Context) error {
	exepath, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", windowsServiceName)
	}
	s, err = m.CreateService(windowsServiceName, exepath, mgr.Config{DisplayName: windowsServiceDescription}, "is", "auto-started")
	if err != nil {
		return err
	}
	defer s.Close()
	err = eventlog.InstallAsEventCreate(windowsServiceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return fmt.Errorf("SetupEventLogSource() failed: %s", err)
	}
	return nil
}

func uninstallWindowsService(c *cli.Context) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", windowsServiceName)
	}
	defer s.Close()
	err = s.Delete()
	if err != nil {
		return err
	}
	err = eventlog.Remove(windowsServiceName)
	if err != nil {
		return fmt.Errorf("RemoveEventLogSource() failed: %s", err)
	}
	return nil
}
