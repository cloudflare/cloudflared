package token

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/transfer"
	"github.com/cloudflare/cloudflared/log"
	"github.com/coreos/go-oidc/jose"
	"github.com/coreos/go-oidc/oidc"
	homedir "github.com/mitchellh/go-homedir"
)

var logger = log.CreateLogger()

// FetchToken will either load a stored token or generate a new one
func FetchToken(appURL *url.URL) (string, error) {
	if token, err := GetTokenIfExists(appURL); token != "" && err == nil {
		return token, nil
	}

	path, err := generateFilePathForTokenURL(appURL)
	if err != nil {
		return "", err
	}

	// this weird parameter is the resource name (token) and the key/value
	// we want to send to the transfer service. the key is token and the value
	// is blank (basically just the id generated in the transfer service)
	const resourceName, key, value = "token", "token", ""
	token, err := transfer.Run(appURL, resourceName, key, value, path, true)
	if err != nil {
		return "", err
	}

	return string(token), nil
}

// GetTokenIfExists will return the token from local storage if it exists
func GetTokenIfExists(url *url.URL) (string, error) {
	path, err := generateFilePathForTokenURL(url)
	if err != nil {
		return "", err
	}
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	token, err := jose.ParseJWT(string(content))
	if err != nil {
		return "", err
	}

	claims, err := token.Claims()
	if err != nil {
		return "", err
	}
	ident, err := oidc.IdentityFromClaims(claims)
	if err == nil && ident.ExpiresAt.After(time.Now()) {
		return token.Encode(), nil
	}
	return "", err
}

// generateFilePathForTokenURL will return a filepath for given access application url
func generateFilePathForTokenURL(url *url.URL) (string, error) {
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
	name := strings.Replace(fmt.Sprintf("%s%s-token", url.Hostname(), url.EscapedPath()), "/", "-", -1)
	return filepath.Join(configPath, name), nil
}
