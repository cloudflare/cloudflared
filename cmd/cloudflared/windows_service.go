// +build windows

package main

// Copypasta from the example files:
// https://github.com/golang/sys/blob/master/windows/svc/example

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/logger"
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

	// ERROR_FAILED_SERVICE_CONTROLLER_CONNECT
	// https://docs.microsoft.com/en-us/windows/desktop/debug/system-error-codes--1000-1299-
	serviceControllerConnectionFailure = 1063

	LogFieldWindowsServiceName = "windowsServiceName"
)

func runApp(app *cli.App, graceShutdownC chan struct{}) {
	app.Commands = append(app.Commands, &cli.Command{
		Name:  "service",
		Usage: "Manages the Argo Tunnel Windows service",
		Subcommands: []*cli.Command{
			{
				Name:   "install",
				Usage:  "Install Argo Tunnel as a Windows service",
				Action: cliutil.ConfiguredAction(installWindowsService),
			},
			{
				Name:   "uninstall",
				Usage:  "Uninstall the Argo Tunnel service",
				Action: cliutil.ConfiguredAction(uninstallWindowsService),
			},
		},
	})

	// `IsAnInteractiveSession()` isn't exactly equivalent to "should the
	// process run as a normal EXE?" There are legitimate non-service cases,
	// like running cloudflared in a GCP startup script, for which
	// `IsAnInteractiveSession()` returns false. For more context, see:
	//     https://github.com/judwhite/go-svc/issues/6
	// It seems that the "correct way" to check "is this a normal EXE?" is:
	//     1. attempt to connect to the Service Control Manager
	//     2. get ERROR_FAILED_SERVICE_CONTROLLER_CONNECT
	// This involves actually trying to start the service.

	log := logger.Create(nil)

	isIntSess, err := svc.IsAnInteractiveSession()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to determine if we are running in an interactive session")
	}
	if isIntSess {
		app.Run(os.Args)
		return
	}

	// Run executes service name by calling windowsService which is a Handler
	// interface that implements Execute method.
	// It will set service status to stop after Execute returns
	err = svc.Run(windowsServiceName, &windowsService{app: app, graceShutdownC: graceShutdownC})
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && int(errno) == serviceControllerConnectionFailure {
			// Hack: assume this is a false negative from the IsAnInteractiveSession() check above.
			// Run the app in "interactive" mode anyway.
			app.Run(os.Args)
			return
		}
		log.Fatal().Err(err).Msgf("%s service failed", windowsServiceName)
	}
}

type windowsService struct {
	app            *cli.App
	graceShutdownC chan struct{}
}

// Execute is called by the service manager when service starts, the state
// of the service will be set to Stopped when this function returns.
func (s *windowsService) Execute(serviceArgs []string, r <-chan svc.ChangeRequest, statusChan chan<- svc.Status) (ssec bool, errno uint32) {
	log := logger.Create(nil)
	elog, err := eventlog.Open(windowsServiceName)
	if err != nil {
		log.Err(err).Msgf("Cannot open event log for %s", windowsServiceName)
		return
	}
	defer elog.Close()

	elog.Info(1, fmt.Sprintf("%s service starting", windowsServiceName))
	defer func() {
		elog.Info(1, fmt.Sprintf("%s service stopped", windowsServiceName))
	}()

	// the arguments passed here are only meaningful if they were manually
	// specified by the user, e.g. using the Services console or `sc start`.
	// https://docs.microsoft.com/en-us/windows/desktop/services/service-entry-point
	// https://stackoverflow.com/a/6235139
	var args []string
	if len(serviceArgs) > 1 {
		args = serviceArgs
	} else {
		// fall back to the arguments from ImagePath (or, as sc calls it, binPath)
		args = os.Args
	}
	elog.Info(1, fmt.Sprintf("%s service arguments: %v", windowsServiceName, args))

	statusChan <- svc.Status{State: svc.StartPending}
	errC := make(chan error)
	go func() {
		errC <- s.app.Run(args)
	}()
	statusChan <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				statusChan <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				if s.graceShutdownC != nil {
					// start graceful shutdown
					elog.Info(1, "cloudflared starting graceful shutdown")
					close(s.graceShutdownC)
					s.graceShutdownC = nil
					statusChan <- svc.Status{State: svc.StopPending}
					continue
				}
				// repeated attempts at graceful shutdown forces immediate stop
				elog.Info(1, "cloudflared terminating immediately")
				statusChan <- svc.Status{State: svc.StopPending}
				return false, 0
			default:
				elog.Error(1, fmt.Sprintf("unexpected control request #%d", c))
			}
		case err := <-errC:
			if err != nil {
				elog.Error(1, fmt.Sprintf("cloudflared terminated with error %v", err))
				ssec = true
				errno = 1
			} else {
				elog.Info(1, "cloudflared terminated without error")
				errno = 0
			}
			return
		}
	}
}

func installWindowsService(c *cli.Context) error {
	zeroLogger := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	zeroLogger.Info().Msg("Installing Argo Tunnel Windows service")
	exepath, err := os.Executable()
	if err != nil {
		return errors.Wrap(err, "Cannot find path name that start the process")
	}
	m, err := mgr.Connect()
	if err != nil {
		return errors.Wrap(err, "Cannot establish a connection to the service control manager")
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	log := zeroLogger.With().Str(LogFieldWindowsServiceName, windowsServiceName).Logger()
	if err == nil {
		s.Close()
		return fmt.Errorf("Service %s already exists", windowsServiceName)
	}
	config := mgr.Config{StartType: mgr.StartAutomatic, DisplayName: windowsServiceDescription}
	s, err = m.CreateService(windowsServiceName, exepath, config)
	if err != nil {
		return errors.Wrap(err, "Cannot install service")
	}
	defer s.Close()
	log.Info().Msg("Argo Tunnel agent service is installed")
	err = eventlog.InstallAsEventCreate(windowsServiceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		s.Delete()
		return errors.Wrap(err, "Cannot install event logger")
	}
	err = configRecoveryOption(s.Handle)
	if err != nil {
		log.Err(err).Msg("Cannot set service recovery actions")
		log.Info().Msgf("See %s to manually configure service recovery actions", windowsServiceUrl)
	}
	return nil
}

func uninstallWindowsService(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog).
		With().
		Str(LogFieldWindowsServiceName, windowsServiceName).Logger()

	log.Info().Msg("Uninstalling Argo Tunnel Windows Service")
	m, err := mgr.Connect()
	if err != nil {
		return errors.Wrap(err, "Cannot establish a connection to the service control manager")
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return fmt.Errorf("Service %s is not installed", windowsServiceName)
	}
	defer s.Close()
	err = s.Delete()
	if err != nil {
		return errors.Wrap(err, "Cannot delete service")
	}
	log.Info().Msg("Argo Tunnel agent service is uninstalled")
	err = eventlog.Remove(windowsServiceName)
	if err != nil {
		return errors.Wrap(err, "Cannot remove event logger")
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
