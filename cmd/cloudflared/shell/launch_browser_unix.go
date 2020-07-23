//+build linux freebsd openbsd netbsd

package shell

import (
	"os"
	"os/exec"
)

func getBrowserCmd(url string) *exec.Cmd {
	// Check for Windows Subsystem for Linux (v2+).
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	}

	return exec.Command("xdg-open", url)
}
