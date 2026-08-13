//go:build windows

package main

// Copypasta from the example files:
// https://github.com/golang/sys/blob/master/windows/svc/example

import (
	"fmt"
	"os"
	"path/filepath"
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
	windowsServiceDescription = "Cloudflared agent"
	windowsServiceUrl         = "https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/configure-tunnels/local-management/as-a-service/windows/"

	// Env var that points to a directory for storing application-specific
	// configuration and data (analogous to /etc/). Normally this points to
	// C:\ProgramData.
	programDataEnvVar = "PROGRAMDATA"
	configDirName     = "cloudflared"

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
		Usage: "Manages the cloudflared Windows service",
		Subcommands: []*cli.Command{
			{
				Name:      "install",
				Usage:     "Install cloudflared as a Windows service",
				ArgsUsage: "[TOKEN]",
				Description: `
Installs cloudflared as a Windows service

A token may optionally be provided. If a token is provided, it will be written
to disk in the service configuration directory and the cloudflared service
configured to use it via the --token-file argument.

If no token is provided, cloudflared will run without the --token-file argument,
causing it to look for credentials in a configuration file upon startup.`,
				Action: cliutil.ConfiguredAction(installWindowsService),
			},
			{
				Name:   "uninstall",
				Usage:  "Uninstall the cloudflared service",
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

// Creates the token file at the given path, restricting its permissions by
// modifying its Windows ACLs. We change the ACLs on the token file such that
// the Administrators group and SYSTEM account (which is what cloudflared runs
// as) have full access and all others are denied, with the Administrator group
// owning the file.
func createTokenFile(path string) error {
	// This is a Windows Security Descriptor string describing the permissions
	// we apply to the token file. This is the domain-specific language Windows
	// uses for representing access rights.
	//
	// - O:BA         -> Set the owner to the builtin administrators group (BA)
	// - D:           -> Start of discretionary access control list describing access rights
	// - P            -> Set the SE_DACL_PROTECTED flag, which prevents the file from
	//                   inheriting the (usually permissive) ACEs from its parent directory
	// - (A;;FA;;;BA) -> ACE #1: Allow (A) Full access (FA) to the Builtin Administrators group (BA)
	// - (A;;FA;;;SY) -> ACE #2: Ditto but for the Local System user (SY)
	//
	// Relevant Docs:
	//
	// - SecurityDescriptor string as a whole:
	//     https://learn.microsoft.com/en-us/windows/win32/secauthz/security-descriptor-string-format
	// - SID Strings such as BA/SY
	//     https://learn.microsoft.com/en-us/windows/win32/secauthz/sid-strings
	// - ACE Strings such as (A;;FA;;BA)
	//     https://learn.microsoft.com/en-us/windows/win32/secauthz/ace-strings
	const sdString = "O:BAD:P(A;;FA;;;BA)(A;;FA;;;SY)"
	sd, err := windows.SecurityDescriptorFromString(sdString)
	if err != nil {
		return fmt.Errorf("create token security descriptor: %w", err)
	}

	pathRaw, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("convert path to UTF-16: %w", err)
	}

	f, err := windows.CreateFile(
		pathRaw,
		windows.GENERIC_WRITE,
		0,
		&windows.SecurityAttributes{
			Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
			SecurityDescriptor: sd,
			InheritHandle:      0,
		},
		windows.CREATE_ALWAYS, // Will truncate the file if it exists
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)

	if err != nil {
		return fmt.Errorf("create token file: %w", err)
	}

	if err := windows.CloseHandle(f); err != nil {
		return fmt.Errorf("close token file: %w", err)
	}

	// As with os.CreateFile / os.OpenFile on Unix, if the file already exists
	// windows.CreateFile will not update the permission information, so we do
	// that explicitly after creating the file.

	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("get token file owner: %w", err)
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("get token file DACL: %w", err)
	}

	// Bitmask indicating which security info we want to set on the file:
	//
	// OWNER_SECURITY_INFORMATION
	//	-> Set file owner
	// DACL_SECURITY_INFORMATION
	// 	-> Set ACEs
	// PROTECTED_DACL_SECURITY_INFORMATION
	//  -> Update DACL to be "protected' such that it cannot inherit entries from its parent
	const securityInfo = windows.OWNER_SECURITY_INFORMATION |
		windows.DACL_SECURITY_INFORMATION |
		windows.PROTECTED_DACL_SECURITY_INFORMATION

	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		securityInfo,
		owner,
		nil,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("set token file security info: %w", err)
	}

	return nil
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

func getConfigDir() (string, error) {
	progDat, progDatSet := os.LookupEnv(programDataEnvVar)
	if !progDatSet {
		return "", fmt.Errorf("could not find program data directory, %s env var must be set", programDataEnvVar)
	}

	return filepath.Join(progDat, configDirName), nil
}

func installWindowsService(c *cli.Context) error {
	zeroLogger := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	zeroLogger.Info().Msg("Installing cloudflared Windows service")
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
		return errors.New(serviceAlreadyExistsWarn(windowsServiceName))
	}
	var extraArgs []string
	if c.NArg() > 0 {
		// The service has been installed using a token e.g.,
		// $ cloudflared service install <token>
		//
		// Write the token file to a config directory so we can start the
		// service with --token-file.

		// Don't use :=, if we did so we would create a new err variable and
		// shadow the outer one, causing the defer below to not have access to
		// the outer err
		var configDir string
		configDir, err = getConfigDir()
		if err != nil {
			return fmt.Errorf("locate config dir: %w", err)
		}

		// Remove token file if service install fails any point onwards from here
		defer func() {
			if err != nil {
				removeTokenFile(configDir, zeroLogger)
			}
		}()

		if err = writeTokenToConfigDir(c, configDir); err != nil {
			return fmt.Errorf("write token to configuration directory at %s: %w", configDir, err)
		}

		extraArgs = buildArgsForTokenFile(configDir)
	}

	config := mgr.Config{StartType: mgr.StartAutomatic, DisplayName: windowsServiceDescription}
	s, err = m.CreateService(windowsServiceName, exepath, config, extraArgs...)
	if err != nil {
		return errors.Wrap(err, "Cannot install service")
	}
	defer s.Close()
	log.Info().Msg("cloudflared agent service is installed")
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

	err = s.Start()
	if err != nil {
		s.Delete()
		return errors.Wrap(err, "Cannot start service")
	}

	log.Info().Msg("Agent service for cloudflared installed successfully")
	return nil
}

func uninstallWindowsService(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog).
		With().
		Str(LogFieldWindowsServiceName, windowsServiceName).Logger()

	log.Info().Msg("Uninstalling cloudflared agent service")
	m, err := mgr.Connect()
	if err != nil {
		return errors.Wrap(err, "Cannot establish a connection to the service control manager")
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return fmt.Errorf("agent service %s is not installed, so it could not be uninstalled", windowsServiceName)
	}
	defer s.Close()

	if status, err := s.Query(); err == nil && status.State == svc.Running {
		log.Info().Msg("Stopping cloudflared agent service")
		if _, err := s.Control(svc.Stop); err != nil {
			log.Info().Err(err).Msg("Failed to stop cloudflared agent service, you may need to stop it manually to complete uninstall.")
		}
	}

	err = s.Delete()
	if err != nil {
		return errors.Wrap(err, "Cannot delete agent service")
	}
	log.Info().Msg("Agent service for cloudflared was uninstalled successfully")
	err = eventlog.Remove(windowsServiceName)
	if err != nil {
		return errors.Wrap(err, "Cannot remove event logger")
	}

	configDir, err := getConfigDir()
	if err != nil {
		// We don't need to hard-error out here, this isn't critical, but we should log it
		log.Warn().Err(err).Msgf("Failed to find configuration directory, not removing secret token file")
	} else {
		removeTokenFile(configDir, &log)
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
