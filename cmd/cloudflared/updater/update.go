package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/facebookgo/grace/gracenet"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
)

const (
	DefaultCheckUpdateFreq        = time.Hour * 24
	noUpdateInShellMessage        = "cloudflared will not automatically update when run from the shell. To enable auto-updates, run cloudflared as a service: https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/run-tunnel/as-a-service/"
	noUpdateOnWindowsMessage      = "cloudflared will not automatically update on Windows systems."
	noUpdateManagedPackageMessage = "cloudflared will not automatically update if installed by a package manager."
	isManagedInstallFile          = ".installedFromPackageManager"
	UpdateURL                     = "https://update.argotunnel.com"
	StagingUpdateURL              = "https://staging-update.argotunnel.com"

	LogFieldVersion = "version"
)

var (
	version                string
	BuiltForPackageManager = ""
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
	updateDisabled  bool
	isBeta          bool
	isStaging       bool
	isForced        bool
	intendedVersion string
}

type UpdateOutcome struct {
	Updated     bool
	Version     string
	UserMessage string
	Error       error
}

func (uo *UpdateOutcome) noUpdate() bool {
	return uo.Error == nil && uo.Updated == false
}

func Init(v string) {
	version = v
}

func CheckForUpdate(options updateOptions) (CheckResult, error) {
	cfdPath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	url := UpdateURL
	if options.isStaging {
		url = StagingUpdateURL
	}

	if runtime.GOOS == "windows" {
		cfdPath = encodeWindowsPath(cfdPath)
	}

	s := NewWorkersService(version, url, cfdPath, Options{IsBeta: options.isBeta,
		IsForced: options.isForced, RequestedVersion: options.intendedVersion})

	return s.Check()
}
func encodeWindowsPath(path string) string {
	// We do this because Windows allows spaces in directories such as
	// Program Files but does not allow these directories to be spaced in batch files.
	targetPath := strings.Replace(path, "Program Files (x86)", "PROGRA~2", -1)
	// This is to do the same in 32 bit systems. We do this second so that the first
	// replace is for x86 dirs.
	targetPath = strings.Replace(targetPath, "Program Files", "PROGRA~1", -1)
	return targetPath
}

func applyUpdate(options updateOptions, update CheckResult) UpdateOutcome {
	if update.Version() == "" || options.updateDisabled {
		return UpdateOutcome{UserMessage: update.UserMessage()}
	}

	err := update.Apply()
	if err != nil {
		return UpdateOutcome{Error: err}
	}

	return UpdateOutcome{Updated: true, Version: update.Version(), UserMessage: update.UserMessage()}
}

// Update is the handler for the update command from the command line
func Update(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	if wasInstalledFromPackageManager() {
		packageManagerName := "a package manager"
		if BuiltForPackageManager != "" {
			packageManagerName = BuiltForPackageManager
		}
		log.Error().Msg(fmt.Sprintf("cloudflared was installed by %s. Please update using the same method.", packageManagerName))
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

	updateOutcome := loggedUpdate(log, updateOptions{
		updateDisabled:  false,
		isBeta:          isBeta,
		isStaging:       isStaging,
		isForced:        isForced,
		intendedVersion: c.String("version"),
	})
	if updateOutcome.Error != nil {
		return &statusErr{updateOutcome.Error}
	}

	if updateOutcome.noUpdate() {
		log.Info().Str(LogFieldVersion, updateOutcome.Version).Msg("cloudflared is up to date")
		return nil
	}

	return &statusSuccess{newVersion: updateOutcome.Version}
}

// Checks for an update and applies it if one is available
func loggedUpdate(log *zerolog.Logger, options updateOptions) UpdateOutcome {
	checkResult, err := CheckForUpdate(options)
	if err != nil {
		log.Err(err).Msg("update check failed")
		return UpdateOutcome{Error: err}
	}

	updateOutcome := applyUpdate(options, checkResult)
	if updateOutcome.Updated {
		log.Info().Str(LogFieldVersion, updateOutcome.Version).Msg("cloudflared has been updated")
	}
	if updateOutcome.Error != nil {
		log.Err(updateOutcome.Error).Msg("update failed to apply")
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

func NewAutoUpdater(updateDisabled bool, freq time.Duration, listeners *gracenet.Net, log *zerolog.Logger) *AutoUpdater {
	return &AutoUpdater{
		configurable:     createUpdateConfig(updateDisabled, freq, log),
		listeners:        listeners,
		updateConfigChan: make(chan *configurable),
		log:              log,
	}
}

func createUpdateConfig(updateDisabled bool, freq time.Duration, log *zerolog.Logger) *configurable {
	if isAutoupdateEnabled(log, updateDisabled, freq) {
		log.Info().Dur("autoupdateFreq", freq).Msg("Autoupdate frequency is set")
		return &configurable{
			enabled: true,
			freq:    freq,
		}
	} else {
		return &configurable{
			enabled: false,
			freq:    DefaultCheckUpdateFreq,
		}
	}
}

func (a *AutoUpdater) Run(ctx context.Context) error {
	ticker := time.NewTicker(a.configurable.freq)
	for {
		updateOutcome := loggedUpdate(a.log, updateOptions{updateDisabled: !a.configurable.enabled})
		if updateOutcome.Updated {
			Init(updateOutcome.Version)
			if IsSysV() {
				// SysV doesn't have a mechanism to keep service alive, we have to restart the process
				a.log.Info().Msg("Restarting service managed by SysV...")
				pid, err := a.listeners.StartProcess()
				if err != nil {
					a.log.Err(err).Msg("Unable to restart server automatically")
					return &statusErr{err: err}
				}
				// stop old process after autoupdate. Otherwise we create a new process
				// after each update
				a.log.Info().Msgf("PID of the new process is %d", pid)
			}
			return &statusSuccess{newVersion: updateOutcome.Version}
		} else if updateOutcome.UserMessage != "" {
			a.log.Warn().Msg(updateOutcome.UserMessage)
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
func (a *AutoUpdater) Update(updateDisabled bool, newFreq time.Duration) {
	a.updateConfigChan <- createUpdateConfig(updateDisabled, newFreq, a.log)
}

func isAutoupdateEnabled(log *zerolog.Logger, updateDisabled bool, updateFreq time.Duration) bool {
	if !supportAutoUpdate(log) {
		return false
	}
	return !updateDisabled && updateFreq != 0
}

func supportAutoUpdate(log *zerolog.Logger) bool {
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
	return len(BuiltForPackageManager) != 0 || ok
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
