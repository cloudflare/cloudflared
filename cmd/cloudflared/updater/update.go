package updater

import (
	"context"
	"fmt"
	"github.com/rs/zerolog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/facebookgo/grace/gracenet"
)

const (
	DefaultCheckUpdateFreq        = time.Hour * 24
	noUpdateInShellMessage        = "cloudflared will not automatically update when run from the shell. To enable auto-updates, run cloudflared as a service: https://developers.cloudflare.com/argo-tunnel/reference/service/"
	noUpdateOnWindowsMessage      = "cloudflared will not automatically update on Windows systems."
	noUpdateManagedPackageMessage = "cloudflared will not automatically update if installed by a package manager."
	isManagedInstallFile          = ".installedFromPackageManager"
	UpdateURL                     = "https://update.argotunnel.com"
	StagingUpdateURL              = "https://staging-update.argotunnel.com"
)

var (
	version string
)

// BinaryUpdated implements ExitCoder interface, the app will exit with status code 11
// https://pkg.go.dev/github.com/urfave/cli/v2?tab=doc#ExitCoder
type statusSuccess struct {
	newVersion string
}

func (u *statusSuccess) Error() string {
	return fmt.Sprintf("cloudflared has been updated to version %s", u.newVersion)
}

func (u *statusSuccess) ExitCode() int {
	return 11
}

// UpdateErr implements ExitCoder interface, the app will exit with status code 10
type statusErr struct {
	err error
}

func (e *statusErr) Error() string {
	return fmt.Sprintf("failed to update cloudflared: %v", e.err)
}

func (e *statusErr) ExitCode() int {
	return 10
}

type updateOptions struct {
	isBeta    bool
	isStaging bool
	isForced  bool
	version   string
}

type UpdateOutcome struct {
	Updated bool
	Version string
	Error   error
}

func (uo *UpdateOutcome) noUpdate() bool {
	return uo.Error == nil && uo.Updated == false
}

func Init(v string) {
	version = v
}

func checkForUpdateAndApply(options updateOptions) UpdateOutcome {
	cfdPath, err := os.Executable()
	if err != nil {
		return UpdateOutcome{Error: err}
	}

	url := UpdateURL
	if options.isStaging {
		url = StagingUpdateURL
	}

	s := NewWorkersService(version, url, cfdPath, Options{IsBeta: options.isBeta,
		IsForced: options.isForced, RequestedVersion: options.version})

	v, err := s.Check()
	if err != nil {
		return UpdateOutcome{Error: err}
	}

	//already on the latest version
	if v == nil {
		return UpdateOutcome{}
	}

	err = v.Apply()
	if err != nil {
		return UpdateOutcome{Error: err}
	}

	return UpdateOutcome{Updated: true, Version: v.String()}
}

// Update is the handler for the update command from the command line
func Update(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	if wasInstalledFromPackageManager() {
		log.Error().Msg("cloudflared was installed by a package manager. Please update using the same method.")
		return nil
	}

	isBeta := c.Bool("beta")
	if isBeta {
		log.Info().Msg("cloudflared is set to update to the latest beta version")
	}

	isStaging := c.Bool("staging")
	if isStaging {
		log.Info().Msg("cloudflared is set to update from staging")
	}

	isForced := c.Bool("force")
	if isForced {
		log.Info().Msg("cloudflared is set to upgrade to the latest publish version regardless of the current version")
	}

	updateOutcome := loggedUpdate(log, updateOptions{isBeta: isBeta, isStaging: isStaging, isForced: isForced, version: c.String("version")})
	if updateOutcome.Error != nil {
		return &statusErr{updateOutcome.Error}
	}

	if updateOutcome.noUpdate() {
		log.Info().Msgf("cloudflared is up to date (%s)", updateOutcome.Version)
		return nil
	}

	return &statusSuccess{newVersion: updateOutcome.Version}
}

// Checks for an update and applies it if one is available
func loggedUpdate(log *zerolog.Logger, options updateOptions) UpdateOutcome {
	updateOutcome := checkForUpdateAndApply(options)
	if updateOutcome.Updated {
		log.Info().Msgf("cloudflared has been updated to version %s", updateOutcome.Version)
	}
	if updateOutcome.Error != nil {
		log.Error().Msgf("update check failed: %s", updateOutcome.Error)
	}

	return updateOutcome
}

// AutoUpdater periodically checks for new version of cloudflared.
type AutoUpdater struct {
	configurable     *configurable
	listeners        *gracenet.Net
	updateConfigChan chan *configurable
	log              *zerolog.Logger
}

// AutoUpdaterConfigurable is the attributes of AutoUpdater that can be reconfigured during runtime
type configurable struct {
	enabled bool
	freq    time.Duration
}

func NewAutoUpdater(freq time.Duration, listeners *gracenet.Net, log *zerolog.Logger) *AutoUpdater {
	updaterConfigurable := &configurable{
		enabled: true,
		freq:    freq,
	}
	if freq == 0 {
		updaterConfigurable.enabled = false
		updaterConfigurable.freq = DefaultCheckUpdateFreq
	}
	return &AutoUpdater{
		configurable:     updaterConfigurable,
		listeners:        listeners,
		updateConfigChan: make(chan *configurable),
		log:              log,
	}
}

func (a *AutoUpdater) Run(ctx context.Context) error {
	ticker := time.NewTicker(a.configurable.freq)
	for {
		if a.configurable.enabled {
			updateOutcome := loggedUpdate(a.log, updateOptions{})
			if updateOutcome.Updated {
				if IsSysV() {
					// SysV doesn't have a mechanism to keep service alive, we have to restart the process
					a.log.Info().Msg("Restarting service managed by SysV...")
					pid, err := a.listeners.StartProcess()
					if err != nil {
						a.log.Error().Msgf("Unable to restart server automatically: %s", err)
						return &statusErr{err: err}
					}
					// stop old process after autoupdate. Otherwise we create a new process
					// after each update
					a.log.Info().Msgf("PID of the new process is %d", pid)
				}
				return &statusSuccess{newVersion: updateOutcome.Version}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case newConfigurable := <-a.updateConfigChan:
			ticker.Stop()
			a.configurable = newConfigurable
			ticker = time.NewTicker(a.configurable.freq)
			// Check if there is new version of cloudflared after receiving new AutoUpdaterConfigurable
		case <-ticker.C:
		}
	}
}

// Update is the method to pass new AutoUpdaterConfigurable to a running AutoUpdater. It is safe to be called concurrently
func (a *AutoUpdater) Update(newFreq time.Duration) {
	newConfigurable := &configurable{
		enabled: true,
		freq:    newFreq,
	}
	// A ero duration means autoupdate is disabled
	if newFreq == 0 {
		newConfigurable.enabled = false
		newConfigurable.freq = DefaultCheckUpdateFreq
	}
	a.updateConfigChan <- newConfigurable
}

func IsAutoupdateEnabled(c *cli.Context, log *zerolog.Logger) bool {
	if !SupportAutoUpdate(log) {
		return false
	}
	return !c.Bool("no-autoupdate") && c.Duration("autoupdate-freq") != 0
}

func SupportAutoUpdate(log *zerolog.Logger) bool {
	if runtime.GOOS == "windows" {
		log.Info().Msg(noUpdateOnWindowsMessage)
		return false
	}

	if wasInstalledFromPackageManager() {
		log.Info().Msg(noUpdateManagedPackageMessage)
		return false
	}

	if isRunningFromTerminal() {
		log.Info().Msg(noUpdateInShellMessage)
		return false
	}
	return true
}

func wasInstalledFromPackageManager() bool {
	ok, _ := config.FileExists(filepath.Join(config.DefaultUnixConfigLocation, isManagedInstallFile))
	return ok
}

func isRunningFromTerminal() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}

func IsSysV() bool {
	if runtime.GOOS != "linux" {
		return false
	}

	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return false
	}
	return true
}
