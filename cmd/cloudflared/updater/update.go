package updater

import (
	"context"
	"os"
	"runtime"
	"time"

	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/urfave/cli.v2"

	"github.com/cloudflare/cloudflared/log"
	"github.com/equinox-io/equinox"
	"github.com/facebookgo/grace/gracenet"
)

const (
	DefaultCheckUpdateFreq   = time.Hour * 24
	appID                    = "app_idCzgxYerVD"
	noUpdateInShellMessage   = "cloudflared will not automatically update when run from the shell. To enable auto-updates, run cloudflared as a service: https://developers.cloudflare.com/argo-tunnel/reference/service/"
	noUpdateOnWindowsMessage = "cloudflared will not automatically update on Windows systems."
)

var (
	publicKey = []byte(`
-----BEGIN ECDSA PUBLIC KEY-----
MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAE4OWZocTVZ8Do/L6ScLdkV+9A0IYMHoOf
dsCmJ/QZ6aw0w9qkkwEpne1Lmo6+0pGexZzFZOH6w5amShn+RXt7qkSid9iWlzGq
EKx0BZogHSor9Wy5VztdFaAaVbsJiCbO
-----END ECDSA PUBLIC KEY-----
`)
	logger = log.CreateLogger()
)

type UpdateOutcome struct {
	Updated bool
	Version string
	Error   error
}

func (uo *UpdateOutcome) noUpdate() bool {
	return uo.Error != nil && uo.Updated == false
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
	updateOutcome := loggedUpdate()
	if updateOutcome.Error != nil {
		os.Exit(10)
	}

	if updateOutcome.noUpdate() {
		logger.Infof("cloudflared is up to date (%s)", updateOutcome.Version)
	}

	return updateOutcome.Error
}

// Checks for an update and applies it if one is available
func loggedUpdate() UpdateOutcome {
	updateOutcome := checkForUpdateAndApply()
	if updateOutcome.Updated {
		logger.Infof("cloudflared has been updated to version %s", updateOutcome.Version)
	}
	if updateOutcome.Error != nil {
		logger.WithError(updateOutcome.Error).Error("update check failed")
	}

	return updateOutcome
}

// AutoUpdater periodically checks for new version of cloudflared.
type AutoUpdater struct {
	configurable     *configurable
	listeners        *gracenet.Net
	updateConfigChan chan *configurable
}

// AutoUpdaterConfigurable is the attributes of AutoUpdater that can be reconfigured during runtime
type configurable struct {
	enabled bool
	freq    time.Duration
}

func NewAutoUpdater(freq time.Duration, listeners *gracenet.Net) *AutoUpdater {
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
	}
}

func (a *AutoUpdater) Run(ctx context.Context) error {
	ticker := time.NewTicker(a.configurable.freq)
	for {
		if a.configurable.enabled {
			updateOutcome := loggedUpdate()
			if updateOutcome.Updated {
				os.Args = append(os.Args, "--is-autoupdated=true")
				pid, err := a.listeners.StartProcess()
				if err != nil {
					logger.WithError(err).Error("Unable to restart server automatically")
					return err
				}
				// stop old process after autoupdate. Otherwise we create a new process
				// after each update
				logger.Infof("PID of the new process is %d", pid)
				return nil
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

func IsAutoupdateEnabled(c *cli.Context) bool {
	if !SupportAutoUpdate() {
		return false
	}
	return !c.Bool("no-autoupdate") && c.Duration("autoupdate-freq") != 0
}

func SupportAutoUpdate() bool {
	if runtime.GOOS == "windows" {
		logger.Info(noUpdateOnWindowsMessage)
		return false
	}

	if isRunningFromTerminal() {
		logger.Info(noUpdateInShellMessage)
		return false
	}
	return true
}

func isRunningFromTerminal() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}
