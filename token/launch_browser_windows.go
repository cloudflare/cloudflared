//+build windows

package token

import (
	"fmt"
	"os/exec"
	"syscall"
)

func getBrowserCmd(url string) *exec.Cmd {
	cmd := exec.Command("cmd")
	// CmdLine is only defined when compiling for windows.
	// Empty string is the cmd proc "Title". Needs to be included because the start command will interpret the first
	// quoted string as that field and we want to quote the URL.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: fmt.Sprintf(`/c start "" "%s"`, url)}
	return cmd
}
