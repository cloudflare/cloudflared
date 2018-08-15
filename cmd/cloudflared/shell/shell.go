package shell

import (
	"io"
	"os"
	"os/exec"
	"runtime"
)

// OpenBrowser opens the specified URL in the default browser of the user
func OpenBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}

// Run will kick off a shell task and pipe the results to the respective std pipes
func Run(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	stderr, err := c.StderrPipe()
	if err != nil {
		return err
	}
	go func() {
		io.Copy(os.Stderr, stderr)
	}()

	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	go func() {
		io.Copy(os.Stdout, stdout)
	}()
	return c.Run()
}
