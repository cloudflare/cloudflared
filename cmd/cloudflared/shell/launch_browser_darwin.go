//+build darwin

package shell

import (
	"os/exec"
)

func getBrowserCmd(url string) *exec.Cmd {
	return exec.Command("open", url)
}
