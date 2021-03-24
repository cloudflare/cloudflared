package token

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	homedir "github.com/mitchellh/go-homedir"

	"github.com/cloudflare/cloudflared/config"
)

// GenerateSSHCertFilePathFromURL will return a file path for creating short lived certificates
func GenerateSSHCertFilePathFromURL(url *url.URL, suffix string) (string, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return "", err
	}
	name := strings.Replace(fmt.Sprintf("%s%s-%s", url.Hostname(), url.EscapedPath(), suffix), "/", "-", -1)
	return filepath.Join(configPath, name), nil
}

// GenerateAppTokenFilePathFromURL will return a filepath for given Access org token
func GenerateAppTokenFilePathFromURL(appDomain, aud string, suffix string) (string, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s-%s", appDomain, aud, suffix)
	name = strings.Replace(strings.Replace(name, "/", "-", -1), "*", "-", -1)
	return filepath.Join(configPath, name), nil
}

// generateOrgTokenFilePathFromURL will return a filepath for given Access application token
func generateOrgTokenFilePathFromURL(authDomain string) (string, error) {
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
