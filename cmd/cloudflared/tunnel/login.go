package tunnel

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/transfer"
	homedir "github.com/mitchellh/go-homedir"
	cli "gopkg.in/urfave/cli.v2"
)

const (
	baseLoginURL     = "https://dash.cloudflare.com/argotunnel"
	callbackStoreURL = "https://login.cloudflarewarp.com/"
)

func login(c *cli.Context) error {
	path, ok, err := checkForExistingCert()
	if ok {
		fmt.Fprintf(os.Stdout, "You have an existing certificate at %s which login would overwrite.\nIf this is intentional, please move or delete that file then run this command again.\n", path)
		return nil
	} else if err != nil {
		return err
	}

	loginURL, err := url.Parse(baseLoginURL)
	if err != nil {
		// shouldn't happen, URL is hardcoded
		return err
	}

	_, err = transfer.Run(loginURL, "cert", "callback", callbackStoreURL, path, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write the certificate due to the following error:\n%v\n\nYour browser will download the certificate instead. You will have to manually\ncopy it to the following path:\n\n%s\n", err, path)
		return err
	}

	fmt.Fprintf(os.Stdout, "You have successfully logged in.\nIf you wish to copy your credentials to a server, they have been saved to:\n%s\n", path)
	return nil
}

func checkForExistingCert() (string, bool, error) {
	configPath, err := homedir.Expand(config.DefaultConfigDirs[0])
	if err != nil {
		return "", false, err
	}
	ok, err := config.FileExists(configPath)
	if !ok && err == nil {
		// create config directory if doesn't already exist
		err = os.Mkdir(configPath, 0700)
	}
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(configPath, config.DefaultCredentialFile)
	fileInfo, err := os.Stat(path)
	if err == nil && fileInfo.Size() > 0 {
		return path, true, nil
	}
	if err != nil && err.(*os.PathError).Err != syscall.ENOENT {
		return path, false, err
	}

	return path, false, nil
}
