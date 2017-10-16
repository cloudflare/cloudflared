package main

import (
	"fmt"
	"os"
	"syscall"

	homedir "github.com/mitchellh/go-homedir"
	cli "gopkg.in/urfave/cli.v2"
)

func login(c *cli.Context) error {
	path, err := homedir.Expand(defaultConfigPath)
	if err != nil {
		return err
	}
	fileInfo, err := os.Stat(path)
	if err == nil && fileInfo.Size() > 0 {
		fmt.Fprintf(os.Stderr, `You have an existing config file at %s which login would overwrite.
If this is intentional, please move or delete that file then run this command again.
`, defaultConfigPath)
		return nil
	}
	if err != nil && err.(*os.PathError).Err != syscall.ENOENT {
		return err
	}

	fmt.Fprintln(os.Stderr, "Please visit https://www.cloudflare.com/a/warp to obtain a certificate.")

	return nil
}
