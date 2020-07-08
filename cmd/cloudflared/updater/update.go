package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/urfave/cli.v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/equinox-io/equinox"
	"github.com/facebookgo/grace/gracenet"
	"github.com/pkg/errors"
)

const (
	DefaultCheckUpdateFreq        = time.Hour * 24
	appID                         = "app_idCzgxYerVD"
	noUpdateInShellMessage        = "cloudflared will not automatically update when run from the shell. To enable auto-updates, run cloudflared as a service: https://developers.cloudflare.com/argo-tunnel/reference/service/"
	noUpdateOnWindowsMessage      = "cloudflared will not automatically update on Windows systems."
	noUpdateManagedPackageMessage = "cloudflared will not automatically update if installed by a package manager."
	isManagedInstallFile          = ".installedFromPackageManager"
)

var (
	publicKey = []byte(`
-----BEGIN ECDSA PUBLIC KEY-----
MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAE4OWZocTVZ8Do/L6ScLdkV+9A0IYMHoOf
dsCmJ/QZ6aw0w9qkkwEpne1Lmo6+0pGexZzFZOH6w5amShn+RXt7qkSid9iWlzGq
EKx0BZogHSor9Wy5VztdFaAaVbsJiCbO
-----END ECDSA PUBLIC KEY-----
`)
)

// BinaryUpdated implements ExitCoder interface, the app will exit with status code 11
// https://pkg.go.dev/gopkg.in/urfave/cli.v2?tab=doc#ExitCoder
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

type UpdateOutcome struct {
	Updated bool
	Version string
	Error   error
}

func (uo *UpdateOutcome) noUpdate() bool {
	return uo.Error == nil && uo.Updated == false
}

func checkForUpdateAndApply() UpdateOutcome {
	var opts equinox.Options
	if err := opts.SetPublicKeyPEM(publicKey); err != nil {
		return UpdateOutcome{Error: err}
	}

	resp, err := equinox.Check(appID, opts)
	switch {
	case err == equinox.NotAvailableErr:
		return UpdateOutcome{}
	case err != nil:
		return UpdateOutcome{Error: err}
	}

	err = resp.Apply()
	if err != nil {
		return UpdateOutcome{Error: err}
	}

	return UpdateOutcome{Updated: true, Version: resp.ReleaseVersion}
}

func Update(_ *cli.Context) error {
	logger, err := logger.New()
	if err != nil {
		return errors.Wrap(err, "error setting up logger")
	}

	if wasInstalledFromPackageManager() {
		logger.Error("cloudflared was installed by a package manager. Please update using the same method.")
		return nil
	}

	updateOutcome := loggedUpdate(logger)
	if updateOutcome.Error != nil {
		return &statusErr{updateOutcome.Error}
	}

	if updateOutcome.noUpdate() {
		logger.Infof("cloudflared is up to date (%s)", updateOutcome.Version)
		return nil
	}

	return &statusSuccess{newVersion: updateOutcome.Version}
}

// Checks for an update and applies it if one is available
func loggedUpdate(logger logger.Service) UpdateOutcome {
	updateOutcome := checkForUpdateAndApply()
	if updateOutcome.Updated {
		logger.Infof("cloudflared has been updated to version %s", updateOutcome.Version)
	}
	if updateOutcome.Error != nil {
		logger.Errorf("update check failed: %s", updateOutcome.Error)
	}

	return updateOutcome
}

// AutoUpdater periodically checks for new version of cloudflared.
type AutoUpdater struct {
	configurable     *configurable
	listeners        *gracenet.Net
	updateConfigChan chan *configurable
	logger           logger.Service
}

// AutoUpdaterConfigurable is the attributes of AutoUpdater that can be reconfigured during runtime
type configurable struct {
	enabled bool
	freq    time.Duration
}

func NewAutoUpdater(freq time.Duration, listeners *gracenet.Net, logger logger.Service) *AutoUpdater {
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
		logger:           logger,
	}
}

func (a *AutoUpdater) Run(ctx context.Context) error {
	ticker := time.NewTicker(a.configurable.freq)
	for {
		if a.configurable.enabled {
			updateOutcome := loggedUpdate(a.logger)
			if updateOutcome.Updated {
				os.Args = append(os.Args, "--is-autoupdated=true")
				if IsSysV() {
					// SysV doesn't have a mechanism to keep service alive, we have to restart the process
					a.logger.Info("Restarting service managed by SysV...")
					pid, err := a.listeners.StartProcess()
					if err != nil {
						a.logger.Errorf("Unable to restart server automatically: %s", err)
						return &statusErr{err: err}
					}
					// stop old process after autoupdate. Otherwise we create a new process
					// after each update
					a.logger.Infof("PID of the new process is %d", pid)
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

func IsAutoupdateEnabled(c *cli.Context, l logger.Service) bool {
	if !SupportAutoUpdate(l) {
		return false
	}
	return !c.Bool("no-autoupdate") && c.Duration("autoupdate-freq") != 0
}

func SupportAutoUpdate(logger logger.Service) bool {
	if runtime.GOOS == "windows" {
		logger.Info(noUpdateOnWindowsMessage)
		return false
	}

	if wasInstalledFromPackageManager() {
		logger.Info(noUpdateManagedPackageMessage)
		return false
	}

	if isRunningFromTerminal() {
		logger.Info(noUpdateInShellMessage)
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
