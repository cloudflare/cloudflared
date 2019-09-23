//+build !windows,!darwin,!linux,!netbsd,!freebsd,!openbsd

package shell

import (
	"os/exec"
)

func getBrowserCmd(url string) *exec.Cmd {
	return nil
}
