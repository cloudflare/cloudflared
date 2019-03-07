package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/cloudflare/cloudflared/validation"
	homedir "github.com/mitchellh/go-homedir"
	"gopkg.in/urfave/cli.v2"
	"gopkg.in/urfave/cli.v2/altsrc"
)

var (
	// File names from which we attempt to read configuration.
	DefaultConfigFiles = []string{"config.yml", "config.yaml"}

	// Launchd doesn't set root env variables, so there is default
	// Windows default config dir was ~/cloudflare-warp in documentation; let's keep it compatible
	DefaultConfigDirs = []string{"~/.cloudflared", "~/.cloudflare-warp", "~/cloudflare-warp", "/usr/local/etc/cloudflared", "/etc/cloudflared"}
)

const DefaultCredentialFile = "cert.pem"

// FileExists checks to see if a file exist at the provided path.
func FileExists(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// ignore missing files
			return false, nil
		}
		return false, err
	}
	f.Close()
	return true, nil
}

// FindInputSourceContext pulls the input source from the config flag.
func FindInputSourceContext(context *cli.Context) (altsrc.InputSourceContext, error) {
	if context.String("config") != "" {
		return altsrc.NewYamlSourceFromFile(context.String("config"))
	}
	return nil, nil
}

// FindDefaultConfigPath returns the first path that contains a config file.
// If none of the combination of DefaultConfigDirs and DefaultConfigFiles
// contains a config file, return empty string.
func FindDefaultConfigPath() string {
	for _, configDir := range DefaultConfigDirs {
		for _, configFile := range DefaultConfigFiles {
			dirPath, err := homedir.Expand(configDir)
			if err != nil {
				continue
			}
			path := filepath.Join(dirPath, configFile)
			if ok, _ := FileExists(path); ok {
				return path
			}
		}
	}
	return ""
}

// ValidateUnixSocket ensures --unix-socket param is used exclusively
// i.e. it fails if a user specifies both --url and --unix-socket
func ValidateUnixSocket(c *cli.Context) (string, error) {
	if c.IsSet("unix-socket") && (c.IsSet("url") || c.NArg() > 0) {
		return "", errors.New("--unix-socket must be used exclusivly.")
	}
	return c.String("unix-socket"), nil
}

// ValidateUrl will validate url flag correctness. It can be either from --url or argument
// Notice ValidateUnixSocket, it will enforce --unix-socket is not used with --url or argument
func ValidateUrl(c *cli.Context) (string, error) {
	var url = c.String("url")
	if c.NArg() > 0 {
		if c.IsSet("url") {
			return "", errors.New("Specified origin urls using both --url and argument. Decide which one you want, I can only support one.")
		}
		url = c.Args().Get(0)
	}
	validUrl, err := validation.ValidateUrl(url)
	return validUrl, err
}
