package main

import (
	"os"
	"runtime"
	"time"

	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/urfave/cli.v2"

	"github.com/equinox-io/equinox"
	"github.com/facebookgo/grace/gracenet"
)

const (
	appID                    = "app_idCzgxYerVD"
	noUpdateInShellMessage   = "cloudflared will not automatically update when run from the shell. To enable auto-updates, run cloudflared as a service: https://developers.cloudflare.com/argo-tunnel/reference/service/"
	noUpdateOnWindowsMessage = "cloudflared will not automatically update on Windows systems."
)

var publicKey = []byte(`
-----BEGIN ECDSA PUBLIC KEY-----
MHYwEAYHKoZIzj0CAQYFK4EEACIDYgAE4OWZocTVZ8Do/L6ScLdkV+9A0IYMHoOf
dsCmJ/QZ6aw0w9qkkwEpne1Lmo6+0pGexZzFZOH6w5amShn+RXt7qkSid9iWlzGq
EKx0BZogHSor9Wy5VztdFaAaVbsJiCbO
-----END ECDSA PUBLIC KEY-----
`)

type ReleaseInfo struct {
	Updated bool
	Version string
	Error   error
}

func checkForUpdates() ReleaseInfo {
	var opts equinox.Options
	if err := opts.SetPublicKeyPEM(publicKey); err != nil {
		return ReleaseInfo{Error: err}
	}

	resp, err := equinox.Check(appID, opts)
	switch {
	case err == equinox.NotAvailableErr:
		return ReleaseInfo{}
	case err != nil:
		return ReleaseInfo{Error: err}
	}

	err = resp.Apply()
	if err != nil {
		return ReleaseInfo{Error: err}
	}

	return ReleaseInfo{Updated: true, Version: resp.ReleaseVersion}
}

func update(_ *cli.Context) error {
	if updateApplied() {
		os.Exit(64)
	}
	return nil
}

func autoupdate(freq time.Duration, listeners *gracenet.Net, shutdownC chan struct{}) error {
	tickC := time.Tick(freq)
	for {
		if updateApplied() {
			os.Args = append(os.Args, "--is-autoupdated=true")
			pid, err := listeners.StartProcess()
			if err != nil {
				logger.WithError(err).Error("Unable to restart server automatically")
				return err
			}
			// stop old process after autoupdate. Otherwise we create a new process
			// after each update
			logger.Infof("PID of the new process is %d", pid)
			return nil
		}
		select {
		case <-tickC:
		case <-shutdownC:
			return nil
		}
	}
}

func updateApplied() bool {
	releaseInfo := checkForUpdates()
	if releaseInfo.Updated {
		logger.Infof("Updated to version %s", releaseInfo.Version)
		return true
	}
	if releaseInfo.Error != nil {
		logger.WithError(releaseInfo.Error).Error("Update check failed")
	}
	return false
}

func isAutoupdateEnabled(c *cli.Context) bool {
	if runtime.GOOS == "windows" {
		logger.Info(noUpdateOnWindowsMessage)
		return false
	}

	if isRunningFromTerminal() {
		logger.Info(noUpdateInShellMessage)
		return false
	}

	return !c.Bool("no-autoupdate") && c.Duration("autoupdate-freq") != 0
}

func isRunningFromTerminal() bool {
	return terminal.IsTerminal(int(os.Stdout.Fd()))
}
