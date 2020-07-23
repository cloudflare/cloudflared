//+build linux freebsd openbsd netbsd

package shell

import (
	"os"
	"os/exec"
)

func getBrowserCmd(url string) *exec.Cmd {
	// Check for Windows Subsystem for Linux (v2+).
	if _, err := os.Stat("/usr/bin/wslview"); err == nil {
		return exec.Command("wslview", url)
	}

	return exec.Command("xdg-open", url)
}
