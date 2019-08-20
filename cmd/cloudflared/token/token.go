package token

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/path"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/transfer"
	"github.com/cloudflare/cloudflared/log"
	"github.com/cloudflare/cloudflared/origin"
	"gopkg.in/coreos/go-oidc.v1/jose"
)

const (
	keyName = "token"
)

var logger = log.CreateLogger()

type lock struct {
	lockFilePath string
	backoff      *origin.BackoffHandler
	sigHandler   *signalHandler
}

type signalHandler struct {
	sigChannel chan os.Signal
	signals    []os.Signal
}

func (s *signalHandler) register(handler func()) {
	s.sigChannel = make(chan os.Signal, 1)
	signal.Notify(s.sigChannel, s.signals...)
	go func(s *signalHandler) {
		for range s.sigChannel {
			handler()
		}
	}(s)
}

func (s *signalHandler) deregister() {
	signal.Stop(s.sigChannel)
	close(s.sigChannel)
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
		sigHandler: &signalHandler{
			signals: []os.Signal{syscall.SIGINT, syscall.SIGTERM},
		},
	}
}

func (l *lock) Acquire() error {
	// Intercept SIGINT and SIGTERM to release lock before exiting
	l.sigHandler.register(func() {
		l.deleteLockFile()
		os.Exit(0)
	})

	// Check for a path.lock file
	// if the lock file exists; start polling
	// if not, create the lock file and go through the normal flow.
	// See AUTH-1736 for the reason why we do all this
	for isTokenLocked(l.lockFilePath) {
		if l.backoff.Backoff(context.Background()) {
			continue
		}

		if err := l.deleteLockFile(); err != nil {
			return err
		}
	}

	// Create a lock file so other processes won't also try to get the token at
	// the same time
	if err := ioutil.WriteFile(l.lockFilePath, []byte{}, 0600); err != nil {
		return err
	}
	return nil
}

func (l *lock) deleteLockFile() error {
	if err := os.Remove(l.lockFilePath); err != nil && !os.IsNotExist(err) {
		return errDeleteTokenFailed(l.lockFilePath)
	}
	return nil
}

func (l *lock) Release() error {
	defer l.sigHandler.deregister()
	return l.deleteLockFile()
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

	fileLock := newLock(path)

	err = fileLock.Acquire()
	if err != nil {
		return "", err
	}
	defer fileLock.Release()

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
