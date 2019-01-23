package token

import (
	"io/ioutil"
	"net/url"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/path"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/transfer"
	"github.com/cloudflare/cloudflared/log"
	"github.com/coreos/go-oidc/jose"
	"github.com/coreos/go-oidc/oidc"
)

const (
	keyName = "token"
)

var logger = log.CreateLogger()

// FetchToken will either load a stored token or generate a new one
func FetchToken(appURL *url.URL) (string, error) {
	if token, err := GetTokenIfExists(appURL); token != "" && err == nil {
		return token, nil
	}

	path, err := path.GenerateFilePathFromURL(appURL, keyName)
	if err != nil {
		return "", err
	}

	// this weird parameter is the resource name (token) and the key/value
	// we want to send to the transfer service. the key is token and the value
	// is blank (basically just the id generated in the transfer service)
	token, err := transfer.Run(appURL, keyName, keyName, "", path, true)
	if err != nil {
		return "", err
	}

	return string(token), nil
}

// GetTokenIfExists will return the token from local storage if it exists
func GetTokenIfExists(url *url.URL) (string, error) {
	path, err := path.GenerateFilePathFromURL(url, keyName)
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
	// AUTH-1404, reauth if the token is about to expire within 15 minutes
	if err == nil && ident.ExpiresAt.After(time.Now().Add(time.Minute*15)) {
		return token.Encode(), nil
	}
	return "", err
}
