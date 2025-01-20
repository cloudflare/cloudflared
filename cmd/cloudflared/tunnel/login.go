package tunnel

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	"github.com/cloudflare/cloudflared/config"
	"github.com/cloudflare/cloudflared/credentials"
	"github.com/cloudflare/cloudflared/logger"
	"github.com/cloudflare/cloudflared/token"
)

const (
	baseLoginURL = "https://dash.cloudflare.com/argotunnel"
	callbackURL  = "https://login.cloudflareaccess.org/"
	// For now these are the same but will change in the future once we know which URLs to use (TUN-8872)
	fedBaseLoginURL      = "https://dash.cloudflare.com/argotunnel"
	fedCallbackStoreURL  = "https://login.cloudflareaccess.org/"
	fedRAMPParamName     = "fedramp"
	loginURLParamName    = "loginURL"
	callbackURLParamName = "callbackURL"
)

var (
	loginURL = &cli.StringFlag{
		Name:  loginURLParamName,
		Value: baseLoginURL,
		Usage: "The URL used to login (default is https://dash.cloudflare.com/argotunnel)",
	}
	callbackStore = &cli.StringFlag{
		Name:  callbackURLParamName,
		Value: callbackURL,
		Usage: "The URL used for the callback (default is https://login.cloudflareaccess.org/)",
	}
	fedramp = &cli.BoolFlag{
		Name:    fedRAMPParamName,
		Aliases: []string{"f"},
		Usage:   "Login with FedRAMP High environment.",
	}
)

func buildLoginSubcommand(hidden bool) *cli.Command {
	return &cli.Command{
		Name:      "login",
		Action:    cliutil.ConfiguredAction(login),
		Usage:     "Generate a configuration file with your login details",
		ArgsUsage: " ",
		Hidden:    hidden,
		Flags: []cli.Flag{
			loginURL,
			callbackStore,
			fedramp,
		},
	}
}

func login(c *cli.Context) error {
	log := logger.CreateLoggerFromContext(c, logger.EnableTerminalLog)

	path, ok, err := checkForExistingCert()
	if ok {
		fmt.Fprintf(os.Stdout, "You have an existing certificate at %s which login would overwrite.\nIf this is intentional, please move or delete that file then run this command again.\n", path)
		return nil
	} else if err != nil {
		return err
	}

	var (
		baseloginURL     = c.String(loginURLParamName)
		callbackStoreURL = c.String(callbackURLParamName)
	)

	if c.Bool(fedRAMPParamName) {
		baseloginURL = fedBaseLoginURL
		callbackStoreURL = fedCallbackStoreURL
	}

	loginURL, err := url.Parse(baseloginURL)
	if err != nil {
		return err
	}

	resourceData, err := token.RunTransfer(
		loginURL,
		"",
		"cert",
		"callback",
		callbackStoreURL,
		false,
		false,
		log,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write the certificate due to the following error:\n%v\n\nYour browser will download the certificate instead. You will have to manually\ncopy it to the following path:\n\n%s\n", err, path)
		return err
	}

	if err := os.WriteFile(path, resourceData, 0600); err != nil {
		return errors.Wrap(err, fmt.Sprintf("error writing cert to %s", path))
	}

	fmt.Fprintf(os.Stdout, "You have successfully logged in.\nIf you wish to copy your credentials to a server, they have been saved to:\n%s\n", path)
	return nil
}

func checkForExistingCert() (string, bool, error) {
	configPath, err := homedir.Expand(config.DefaultConfigSearchDirectories()[0])
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
	path := filepath.Join(configPath, credentials.DefaultCredentialFile)
	fileInfo, err := os.Stat(path)
	if err == nil && fileInfo.Size() > 0 {
		return path, true, nil
	}
	if err != nil && err.(*os.PathError).Err != syscall.ENOENT {
		return path, false, err
	}

	return path, false, nil
}
