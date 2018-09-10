// +build windows

package main

// Copypasta from the example files:
// https://github.com/golang/sys/blob/master/windows/svc/example

import (
	"fmt"
	"os"
	"time"
	"unsafe"

	cli "gopkg.in/urfave/cli.v2"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceName        = "Cloudflared"
	windowsServiceDescription = "Argo Tunnel agent"
	windowsServiceUrl         = "https://developers.cloudflare.com/argo-tunnel/reference/service/"

	recoverActionDelay      = time.Second * 20
	failureCountResetPeriod = time.Hour * 24

	// not defined in golang.org/x/sys/windows package
	// https://msdn.microsoft.com/en-us/library/windows/desktop/ms681988(v=vs.85).aspx
	serviceConfigFailureActionsFlag = 4
)

func runApp(app *cli.App, shutdownC, graceShutdownC chan struct{}) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the Argo Tunnel Windows service",
		Subcommands: []*cli.Command{
			&cli.Command{
				Name:   "install",
				Usage:  "Install Argo Tunnel as a Windows service",
				Action: installWindowsService,
			},
			&cli.Command{
				Name:   "uninstall",
				Usage:  "Uninstall the Argo Tunnel service",
				Action: uninstallWindowsService,
			},
		},
	})

	isIntSess, err := svc.IsAnInteractiveSession()
	if err != nil {
		logger.Fatalf("failed to determine if we are running in an interactive session: %v", err)
	}

	if isIntSess {
		app.Run(os.Args)
		return
	}

	elog, err := eventlog.Open(windowsServiceName)
	if err != nil {
		logger.WithError(err).Errorf("Cannot open event log for %s", windowsServiceName)
		return
	}
	defer elog.Close()

	elog.Info(1, fmt.Sprintf("%s service starting", windowsServiceName))
	// Run executes service name by calling windowsService which is a Handler
	// interface that implements Execute method.
	// It will set service status to stop after Execute returns
	err = svc.Run(windowsServiceName, &windowsService{app: app, elog: elog, shutdownC: shutdownC, graceShutdownC: graceShutdownC})
	if err != nil {
		elog.Error(1, fmt.Sprintf("%s service failed: %v", windowsServiceName, err))
		return
	}
	elog.Info(1, fmt.Sprintf("%s service stopped", windowsServiceName))
}

type windowsService struct {
	app            *cli.App
	elog           *eventlog.Log
	shutdownC      chan struct{}
	graceShutdownC chan struct{}
}

// called by the package code at the start of the service
func (s *windowsService) Execute(args []string, r <-chan svc.ChangeRequest, statusChan chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	statusChan <- svc.Status{State: svc.StartPending}
	errC := make(chan error)
	go func() {
		errC <- s.app.Run(args)
	}()
	statusChan <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				s.elog.Info(1, fmt.Sprintf("control request 1 #%d", c))
				statusChan <- c.CurrentStatus
			case svc.Stop:
				s.elog.Info(1, "received stop control request")
				close(s.graceShutdownC)
				statusChan <- svc.Status{State: svc.StopPending}
			case svc.Shutdown:
				s.elog.Info(1, "received shutdown control request")
				close(s.shutdownC)
				statusChan <- svc.Status{State: svc.StopPending}
			default:
				s.elog.Error(1, fmt.Sprintf("unexpected control request #%d", c))
			}
		case err := <-errC:
			ssec = true
			if err != nil {
				s.elog.Error(1, fmt.Sprintf("cloudflared terminated with error %v", err))
				errno = 1
			} else {
				s.elog.Info(1, "cloudflared terminated without error")
				errno = 0
			}
			return
		}
	}
}

func installWindowsService(c *cli.Context) error {
	logger.Infof("Installing Argo Tunnel Windows service")
	exepath, err := os.Executable()
	if err != nil {
		logger.Errorf("Cannot find path name that start the process")
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		logger.WithError(err).Errorf("Cannot establish a connection to the service control manager")
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err == nil {
		s.Close()
		logger.Errorf("service %s already exists", windowsServiceName)
		return fmt.Errorf("service %s already exists", windowsServiceName)
	}
	config := mgr.Config{StartType: mgr.StartAutomatic, DisplayName: windowsServiceDescription}
	s, err = m.CreateService(windowsServiceName, exepath, config)
	if err != nil {
		logger.Errorf("Cannot install service %s", windowsServiceName)
		return err
	}
	defer s.Close()
	logger.Infof("Argo Tunnel agent service is installed")
	err = eventlog.InstallAsEventCreate(windowsServiceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		logger.WithError(err).Errorf("Cannot install event logger")
		return fmt.Errorf("SetupEventLogSource() failed: %s", err)
	}
	err = configRecoveryOption(s.Handle)
	if err != nil {
		logger.WithError(err).Errorf("Cannot set service recovery actions")
		logger.Infof("See %s to manually configure service recovery actions", windowsServiceUrl)
	}
	return nil
}

func uninstallWindowsService(c *cli.Context) error {
	logger.Infof("Uninstalling Argo Tunnel Windows Service")
	m, err := mgr.Connect()
	if err != nil {
		logger.Errorf("Cannot establish a connection to the service control manager")
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		logger.Errorf("service %s is not installed", windowsServiceName)
		return fmt.Errorf("service %s is not installed", windowsServiceName)
	}
	defer s.Close()
	err = s.Delete()
	if err != nil {
		logger.Errorf("Cannot delete service %s", windowsServiceName)
		return err
	}
	logger.Infof("Argo Tunnel agent service is uninstalled")
	err = eventlog.Remove(windowsServiceName)
	if err != nil {
		logger.Errorf("Cannot remove event logger")
		return fmt.Errorf("RemoveEventLogSource() failed: %s", err)
	}
	return nil
}

// defined in https://msdn.microsoft.com/en-us/library/windows/desktop/ms685126(v=vs.85).aspx
type scAction int

// https://msdn.microsoft.com/en-us/library/windows/desktop/ms685126(v=vs.85).aspx
const (
	scActionNone scAction = iota
	scActionRestart
	scActionReboot
	scActionRunCommand
)

// defined in https://msdn.microsoft.com/en-us/library/windows/desktop/ms685939(v=vs.85).aspx
type serviceFailureActions struct {
	// time to wait to reset the failure count to zero if there are no failures in seconds
	resetPeriod uint32
	rebootMsg   *uint16
	command     *uint16
	// If failure count is greater than actionCount, the service controller repeats
	// the last action in actions
	actionCount uint32
	actions     uintptr
}

// https://msdn.microsoft.com/en-us/library/windows/desktop/ms685937(v=vs.85).aspx
// Not supported in Windows Server 2003 and Windows XP
type serviceFailureActionsFlag struct {
	// enableActionsForStopsWithErr is of type BOOL, which is declared as
	// typedef int BOOL in C
	enableActionsForStopsWithErr int
}

type recoveryAction struct {
	recoveryType uint32
	// The time to wait before performing the specified action, in milliseconds
	delay uint32
}

// until https://github.com/golang/go/issues/23239 is release, we will need to
// configure through ChangeServiceConfig2
func configRecoveryOption(handle windows.Handle) error {
	actions := []recoveryAction{
		{recoveryType: uint32(scActionRestart), delay: uint32(recoverActionDelay / time.Millisecond)},
	}
	serviceRecoveryActions := serviceFailureActions{
		resetPeriod: uint32(failureCountResetPeriod / time.Second),
		actionCount: uint32(len(actions)),
		actions:     uintptr(unsafe.Pointer(&actions[0])),
	}
	if err := windows.ChangeServiceConfig2(handle, windows.SERVICE_CONFIG_FAILURE_ACTIONS, (*byte)(unsafe.Pointer(&serviceRecoveryActions))); err != nil {
		return err
	}
	serviceFailureActionsFlag := serviceFailureActionsFlag{enableActionsForStopsWithErr: 1}
	return windows.ChangeServiceConfig2(handle, serviceConfigFailureActionsFlag, (*byte)(unsafe.Pointer(&serviceFailureActionsFlag)))
}
