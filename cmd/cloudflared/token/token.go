package token

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/path"
	"github.com/cloudflare/cloudflared/cmd/cloudflared/transfer"
	"github.com/cloudflare/cloudflared/origin"

	"github.com/coreos/go-oidc/jose"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

const (
	keyName     = "token"
	tokenHeader = "CF_Authorization"
)

type lock struct {
	lockFilePath string
	backoff      *origin.BackoffHandler
	sigHandler   *signalHandler
}

type signalHandler struct {
	sigChannel chan os.Signal
	signals    []os.Signal
}

type appJWTPayload struct {
	Aud   []string `json:"aud"`
	Email string   `json:"email"`
	Exp   int      `json:"exp"`
	Iat   int      `json:"iat"`
	Nbf   int      `json:"nbf"`
	Iss   string   `json:"iss"`
	Type  string   `json:"type"`
	Subt  string   `json:"sub"`
}

type orgJWTPayload struct {
	appJWTPayload
	Aud string `json:"aud"`
}

type transferServiceResponse struct {
	AppToken string `json:"app_token"`
	OrgToken string `json:"org_token"`
}

func (p appJWTPayload) isExpired() bool {
	return int(time.Now().Unix()) > p.Exp
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
		_ = l.deleteLockFile()
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

// FetchTokenWithRedirect will either load a stored token or generate a new one
// it appends the full url as the redirect URL to the access cli request if opening the browser
func FetchTokenWithRedirect(appURL *url.URL, log *zerolog.Logger) (string, error) {
	return getToken(appURL, false, log)
}

// FetchToken will either load a stored token or generate a new one
// it appends the host of the appURL as the redirect URL to the access cli request if opening the browser
func FetchToken(appURL *url.URL, log *zerolog.Logger) (string, error) {
	return getToken(appURL, true, log)
}

// getToken will either load a stored token or generate a new one
func getToken(appURL *url.URL, useHostOnly bool, log *zerolog.Logger) (string, error) {
	if token, err := GetAppTokenIfExists(appURL); token != "" && err == nil {
		return token, nil
	}

	appTokenPath, err := path.GenerateAppTokenFilePathFromURL(appURL, keyName)
	if err != nil {
		return "", errors.Wrap(err, "failed to generate app token file path")
	}

	fileLockAppToken := newLock(appTokenPath)
	if err = fileLockAppToken.Acquire(); err != nil {
		return "", errors.Wrap(err, "failed to acquire app token lock")
	}
	defer fileLockAppToken.Release()

	// check to see if another process has gotten a token while we waited for the lock
	if token, err := GetAppTokenIfExists(appURL); token != "" && err == nil {
		return token, nil
	}

	// If an app token couldnt be found on disk, check for an org token and attempt to exchange it for an app token.
	var orgTokenPath string
	// Get auth domain to format into org token file path
	if authDomain, err := getAuthDomain(appURL); err != nil {
		log.Error().Msgf("failed to get auth domain: %s", err)
	} else {
		orgToken, err := GetOrgTokenIfExists(authDomain)
		if err != nil {
			orgTokenPath, err = path.GenerateOrgTokenFilePathFromURL(authDomain)
			if err != nil {
				return "", errors.Wrap(err, "failed to generate org token file path")
			}

			fileLockOrgToken := newLock(orgTokenPath)
			if err = fileLockOrgToken.Acquire(); err != nil {
				return "", errors.Wrap(err, "failed to acquire org token lock")
			}
			defer fileLockOrgToken.Release()
			// check if an org token has been created since the lock was acquired
			orgToken, err = GetOrgTokenIfExists(authDomain)
		}
		if err == nil {
			if appToken, err := exchangeOrgToken(appURL, orgToken); err != nil {
				log.Debug().Msgf("failed to exchange org token for app token: %s", err)
			} else {
				if err := ioutil.WriteFile(appTokenPath, []byte(appToken), 0600); err != nil {
					return "", errors.Wrap(err, "failed to write app token to disk")
				}
				return appToken, nil
			}
		}
	}
	return getTokensFromEdge(appURL, appTokenPath, orgTokenPath, useHostOnly, log)

}

// getTokensFromEdge will attempt to use the transfer service to retrieve an app and org token, save them to disk,
// and return the app token.
func getTokensFromEdge(appURL *url.URL, appTokenPath, orgTokenPath string, useHostOnly bool, log *zerolog.Logger) (string, error) {
	// If no org token exists or if it couldnt be exchanged for an app token, then run the transfer service flow.

	// this weird parameter is the resource name (token) and the key/value
	// we want to send to the transfer service. the key is token and the value
	// is blank (basically just the id generated in the transfer service)
	resourceData, err := transfer.Run(appURL, keyName, keyName, "", true, useHostOnly, log)
	if err != nil {
		return "", errors.Wrap(err, "failed to run transfer service")
	}
	var resp transferServiceResponse
	if err = json.Unmarshal(resourceData, &resp); err != nil {
		return "", errors.Wrap(err, "failed to marshal transfer service response")
	}

	// If we were able to get the auth domain and generate an org token path, lets write it to disk.
	if orgTokenPath != "" {
		if err := ioutil.WriteFile(orgTokenPath, []byte(resp.OrgToken), 0600); err != nil {
			return "", errors.Wrap(err, "failed to write org token to disk")
		}
	}

	if err := ioutil.WriteFile(appTokenPath, []byte(resp.AppToken), 0600); err != nil {
		return "", errors.Wrap(err, "failed to write app token to disk")
	}

	return resp.AppToken, nil

}

// getAuthDomain makes a request to the appURL and stops at the first redirect. The 302 location header will contain the
// auth domain
func getAuthDomain(appURL *url.URL) (string, error) {
	client := &http.Client{
		// do not follow redirects
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: time.Second * 7,
	}

	authDomainReq, err := http.NewRequest("HEAD", appURL.String(), nil)
	if err != nil {
		return "", errors.Wrap(err, "failed to create auth domain request")
	}
	resp, err := client.Do(authDomainReq)
	if err != nil {
		return "", errors.Wrap(err, "failed to get auth domain")
	}
	resp.Body.Close()
	location, err := resp.Location()
	if err != nil {
		return "", fmt.Errorf("failed to get auth domain. Received status code %d from %s", resp.StatusCode, appURL.String())
	}
	return location.Hostname(), nil

}

// exchangeOrgToken attaches an org token to a request to the appURL and returns an app token. This uses the Access SSO
// flow to automatically generate and return an app token without the login page.
func exchangeOrgToken(appURL *url.URL, orgToken string) (string, error) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// attach org token to login request
			if strings.Contains(req.URL.Path, "cdn-cgi/access/login") {
				req.AddCookie(&http.Cookie{Name: tokenHeader, Value: orgToken})
			}
			// stop after hitting authorized endpoint since it will contain the app token
			if strings.Contains(via[len(via)-1].URL.Path, "cdn-cgi/access/authorized") {
				return http.ErrUseLastResponse
			}
			return nil
		},
		Timeout: time.Second * 7,
	}

	appTokenRequest, err := http.NewRequest("HEAD", appURL.String(), nil)
	if err != nil {
		return "", errors.Wrap(err, "failed to create app token request")
	}
	resp, err := client.Do(appTokenRequest)
	if err != nil {
		return "", errors.Wrap(err, "failed to get app token")
	}
	resp.Body.Close()
	var appToken string
	for _, c := range resp.Cookies() {
		//if Org token revoked on exchange, getTokensFromEdge instead
		validAppToken := c.Name == tokenHeader && time.Now().Before(c.Expires)
		if validAppToken {
			appToken = c.Value
			break
		}
	}

	if len(appToken) > 0 {
		return appToken, nil
	}
	return "", fmt.Errorf("response from %s did not contain app token", resp.Request.URL.String())
}

func GetOrgTokenIfExists(authDomain string) (string, error) {
	path, err := path.GenerateOrgTokenFilePathFromURL(authDomain)
	if err != nil {
		return "", err
	}
	token, err := getTokenIfExists(path)
	if err != nil {
		return "", err
	}
	var payload orgJWTPayload
	err = json.Unmarshal(token.Payload, &payload)
	if err != nil {
		return "", err
	}

	if payload.isExpired() {
		err := os.Remove(path)
		return "", err
	}
	return token.Encode(), nil
}

func GetAppTokenIfExists(url *url.URL) (string, error) {
	path, err := path.GenerateAppTokenFilePathFromURL(url, keyName)
	if err != nil {
		return "", err
	}
	token, err := getTokenIfExists(path)
	if err != nil {
		return "", err
	}
	var payload appJWTPayload
	err = json.Unmarshal(token.Payload, &payload)
	if err != nil {
		return "", err
	}

	if payload.isExpired() {
		err := os.Remove(path)
		return "", err
	}
	return token.Encode(), nil

}

// GetTokenIfExists will return the token from local storage if it exists and not expired
func getTokenIfExists(path string) (*jose.JWT, error) {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	token, err := jose.ParseJWT(string(content))
	if err != nil {
		return nil, err
	}

	return &token, nil
}

// RemoveTokenIfExists removes the a token from local storage if it exists
func RemoveTokenIfExists(url *url.URL) error {
	path, err := path.GenerateAppTokenFilePathFromURL(url, keyName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}
