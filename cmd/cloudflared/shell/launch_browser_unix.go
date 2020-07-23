//+build linux freebsd openbsd netbsd

package shell

import (
	"os"
	"os/exec"
)

func getBrowserCmd(url string) *exec.Cmd {
	// Check if wslview is available (Windows Subsystem for Linux).
	if _, err := os.Stat("/usr/bin/wslview"); err == nil {
		return exec.Command("wslview", url)
	}

	return exec.Command("xdg-open", url)
}
