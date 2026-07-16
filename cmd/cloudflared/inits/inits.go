// Package inits provides detection of the init system managing the host
// (systemd, OpenRC, or SysV). It is shared by the service installer and the
// auto-updater so that init-system detection lives in a single place.
//
// The functions are safe to call on any GOOS; on non-Linux platforms they
// report that no Linux init system is in use.
package inits

import (
	"os"
	"runtime"
)

// IsSystemd reports whether the host is managed by systemd.
func IsSystemd() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

// IsOpenRC reports whether the host is managed by OpenRC.
func IsOpenRC() bool {
	for _, path := range []string{"/sbin/openrc-run", "/usr/sbin/openrc-run", "/usr/bin/openrc-run"} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// IsSysV reports whether the host relies on a SysV-style init system, i.e. a
// Linux host that is managed by neither systemd nor OpenRC. systemd and OpenRC
// keep the service alive themselves, so only SysV needs the process to restart
// itself after an auto-update.
func IsSysV() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	return !IsSystemd() && !IsOpenRC()
}
