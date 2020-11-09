package path

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/mitchellh/go-homedir"
)

// GenerateAppTokenFilePathFromURL will return a filepath for given Access org token
func GenerateAppTokenFilePathFromURL(url *url.URL, suffix string) (string, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return "", err
	}
	name := strings.Replace(fmt.Sprintf("%s%s-%s", url.Hostname(), url.EscapedPath(), suffix), "/", "-", -1)
	return filepath.Join(configPath, name), nil
}

// GenerateOrgTokenFilePathFromURL will return a filepath for given Access application token
func GenerateOrgTokenFilePathFromURL(authDomain string) (string, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return "", err
	}
	name := strings.Replace(fmt.Sprintf("%s-org-token", authDomain), "/", "-", -1)
	return filepath.Join(configPath, name), nil
}

func getConfigPath() (string, error) {
	configPath, err := homedir.Expand(config.DefaultConfigSearchDirectories()[0])
	if err != nil {
		return "", err
	}
	ok, err := config.FileExists(configPath)
	if !ok && err == nil {
		// create config directory if doesn't already exist
		err = os.Mkdir(configPath, 0700)
	}
	return configPath, err
}
