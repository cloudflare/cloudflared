//+build !windows,!darwin,!linux,!netbsd,!freebsd,!openbsd

package token

import (
	"os/exec"
)

func getBrowserCmd(url string) *exec.Cmd {
	return nil
}
