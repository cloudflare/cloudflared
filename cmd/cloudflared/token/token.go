package token

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/path"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/transfer"
	"github.com/cloudflare/cloudflared/log"
	"github.com/cloudflare/cloudflared/origin"
	"github.com/coreos/go-oidc/jose"
)

const (
	keyName = "token"
)

var logger = log.CreateLogger()

type lock struct {
	lockFilePath string
	backoff      *origin.BackoffHandler
}

func errDeleteTokenFailed(lockFilePath string) error {
	return fmt.Errorf("failed to acquire a new Access token. Please try to delete %s", lockFilePath)
}

// newLock will get a new file lock
func newLock(path string) *lock {
	lockPath := path + ".lock"
	return &lock{
		lockFilePath: lockPath,
		backoff:      &origin.BackoffHandler{MaxRetries: 7},
	}
}

func (l *lock) Acquire() error {
	// Check for a path.lock file
	// if the lock file exists; start polling
	// if not, create the lock file and go through the normal flow.
	// See AUTH-1736 for the reason why we do all this
	for isTokenLocked(l.lockFilePath) {
		if l.backoff.Backoff(context.Background()) {
			continue
		} else {
			return errDeleteTokenFailed(l.lockFilePath)
		}
	}

	// Create a lock file so other processes won't also try to get the token at
	// the same time
	if err := ioutil.WriteFile(l.lockFilePath, []byte{}, 0600); err != nil {
		return err
	}
	return nil
}

func (l *lock) Release() error {
	if err := os.Remove(l.lockFilePath); err != nil && !os.IsNotExist(err) {
		return errDeleteTokenFailed(l.lockFilePath)
	}
	return nil
}

// isTokenLocked checks to see if there is another process attempting to get the token already
func isTokenLocked(lockFilePath string) bool {
	exists, err := config.FileExists(lockFilePath)
	return exists && err == nil
}

// FetchToken will either load a stored token or generate a new one
func FetchToken(appURL *url.URL) (string, error) {
	if token, err := GetTokenIfExists(appURL); token != "" && err == nil {
		return token, nil
	}

	path, err := path.GenerateFilePathFromURL(appURL, keyName)
	if err != nil {
		return "", err
	}

	lock := newLock(path)
	err = lock.Acquire()
	if err != nil {
		return "", err
	}
	defer lock.Release()

	// check to see if another process has gotten a token while we waited for the lock
	if token, err := GetTokenIfExists(appURL); token != "" && err == nil {
		return token, nil
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

	return token.Encode(), nil
}

// RemoveTokenIfExists removes the a token from local storage if it exists
func RemoveTokenIfExists(url *url.URL) error {
	path, err := path.GenerateFilePathFromURL(url, keyName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}
