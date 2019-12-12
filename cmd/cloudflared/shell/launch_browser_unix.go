//+build linux freebsd openbsd netbsd

package shell

import (
	"os/exec"
)

func getBrowserCmd(url string) *exec.Cmd {
	return exec.Command("xdg-open", url)
}
