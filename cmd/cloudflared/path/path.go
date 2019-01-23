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

// GenerateFilePathFromURL will return a filepath for given access application url
func GenerateFilePathFromURL(url *url.URL, suffix string) (string, error) {
	configPath, err := homedir.Expand(config.DefaultConfigDirs[0])
	if err != nil {
		return "", err
	}
	ok, err := config.FileExists(configPath)
	if !ok && err == nil {
		// create config directory if doesn't already exist
		err = os.Mkdir(configPath, 0700)
	}
	if err != nil {
		return "", err
	}
	name := strings.Replace(fmt.Sprintf("%s%s-%s", url.Hostname(), url.EscapedPath(), suffix), "/", "-", -1)
	return filepath.Join(configPath, name), nil
}
