package token

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	keyName                    = "token"
	tokenCookie                = "CF_Authorization"
	appSessionCookie           = "CF_AppSession"
	appDomainHeader            = "CF-Access-Domain"
	appAUDHeader               = "CF-Access-Aud"
	AccessLoginWorkerPath      = "/cdn-cgi/access/login"
	AccessAuthorizedWorkerPath = "/cdn-cgi/access/authorized"
)

var (
	userAgent     = "DEV"
	signatureAlgs = []jose.SignatureAlgorithm{jose.RS256}
)

type AppInfo struct {
	AuthDomain string
	AppAUD     string
	AppDomain  string
}

// lockContent is the JSON structure written into lock files.
type lockContent struct {
	PID       int32 `json:"pid"`
	StartTime int64 `json:"start_time"`
}

type jwtPayload struct {
	Aud   []string `json:"-"`
	Email string   `json:"email"`
	Exp   int      `json:"exp"`
	Iat   int      `json:"iat"`
	Nbf   int      `json:"nbf"`
	Iss   string   `json:"iss"`
	Type  string   `json:"type"`
	Subt  string   `json:"sub"`
}

type transferServiceResponse struct {
	AppToken string `json:"app_token"`
	OrgToken string `json:"org_token"`
}

func (p *jwtPayload) UnmarshalJSON(data []byte) error {
	type Alias jwtPayload
	if err := json.Unmarshal(data, (*Alias)(p)); err != nil {
		return err
	}
	var audParser struct {
		Aud any `json:"aud"`
	}
	if err := json.Unmarshal(data, &audParser); err != nil {
		return err
	}
	switch aud := audParser.Aud.(type) {
	case string:
		p.Aud = []string{aud}
	case []any:
		for _, a := range aud {
			s, ok := a.(string)
			if !ok {
				return errors.New("aud array contains non-string elements")
			}
			p.Aud = append(p.Aud, s)
		}
	default:
		return errors.New("aud field is not a string or an array of strings")
	}
	return nil
}

func (p jwtPayload) isExpired() bool {
	return int(time.Now().Unix()) > p.Exp
}

const (
	lockRetryInterval  = 2 * time.Second
	lockTimeout        = 10 * time.Minute
	startTimeTolerance = int64(1000) // milliseconds
)

// acquireLockFile loops until it successfully creates a lock file for the
// given token file path. The lock file is created at tokenPath + ".lock".
//
// On each iteration:
//  1. Try to create the file atomically with O_CREATE|O_EXCL.
//     If that succeeds, write our PID + start time and return nil.
//  2. If the file already exists, read it and check whether the owning
//     process is still alive (PID exists and start time matches).
//  3. If the owner is alive, sleep for lockRetryInterval and retry.
//  4. If the owner is dead (stale lock), remove the file and immediately
//     retry the O_EXCL create. No sleep (the atomic create is the
//     tiebreaker if multiple processes race to reclaim).
func acquireLockFile(tokenPath string, log *zerolog.Logger) error {
	lockPath := tokenPath + ".lock"
	deadline := time.Now().Add(lockTimeout)
	lastURL := ""
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for lock file %s", lockPath)
		}
		err := tryCreateLockFile(lockPath)
		if err == nil {
			log.Debug().Str("path", lockPath).Msg("lock file acquired")
			return nil
		}
		if !os.IsExist(err) {
			return errors.Wrapf(err, "failed to create lock file %s", lockPath)
		}

		// lock file exists, so check if the owner is still alive
		stale, content, checkErr := isLockFileStale(lockPath)
		if checkErr != nil {
			// file may be mid-write by another racer, or was removed
			// between our O_EXCL attempt and this read
			log.Debug().Err(checkErr).Str("path", lockPath).
				Msg("could not read lock file, retrying")
			time.Sleep(lockRetryInterval)
			continue
		}

		if !stale {
			// try to display the auth URL so the user can open a browser
			// manually if the original window is not visible
			if authURL := readAuthURL(tokenPath); authURL != "" && authURL != lastURL {
				fmt.Fprintf(os.Stderr, "\nAnother cloudflared process (pid %d) "+
					"is already waiting for authentication.\n\n"+
					"If a browser window did not open, please visit "+
					"the following URL:\n\n%s\n\n", content.PID, authURL)
				lastURL = authURL
			}
			log.Debug().Str("path", lockPath).
				Msg("lock file is held by another process, retrying")
			time.Sleep(lockRetryInterval)
			continue
		}

		// stale, so remove and immediately retry
		log.Debug().Str("path", lockPath).Int32("stale_pid", content.PID).
			Msg("reclaiming stale lock file")
		if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Debug().Err(removeErr).Str("path", lockPath).
				Msg("could not remove stale lock file, retrying")
			time.Sleep(lockRetryInterval)
			continue
		}
	}
}

// readAuthURL reads the auth URL companion file for the given token path.
// Returns the URL string, or empty string if the file doesn't exist or
// can't be read.
func readAuthURL(tokenPath string) string {
	data, err := os.ReadFile(tokenPath + ".url") // nolint: gosec
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// tryCreateLockFile atomically creates the lock file using O_CREATE|O_EXCL
// and writes the current process's PID and start time into it as JSON.
// The file is created with 0600 permissions (owner read/write only).
func tryCreateLockFile(path string) (retErr error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600) // nolint: gosec
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return
		}
		retErr = f.Close()
	}()

	content, err := newSelfLockContent()
	if err != nil {
		return err
	}

	return json.NewEncoder(f).Encode(content)
}

// newSelfLockContent returns a lockContent describing the current process.
func newSelfLockContent() (lockContent, error) {
	pid := int32(os.Getpid()) // nolint: gosec
	p, err := process.NewProcess(pid)
	if err != nil {
		return lockContent{}, fmt.Errorf("failed to look up own process: %w", err)
	}
	ct, err := p.CreateTime()
	if err != nil {
		return lockContent{}, fmt.Errorf("failed to get own start time: %w", err)
	}
	return lockContent{PID: pid, StartTime: ct}, nil
}

// isLockFileStale reads the lock file and checks whether the owning process
// is dead or has a mismatched start time. Returns (true, content, nil) if
// stale, (false, content, nil) if actively held, or an error if the file
// cannot be read.
func isLockFileStale(path string) (bool, lockContent, error) {
	data, err := os.ReadFile(path) // nolint: gosec
	if err != nil {
		return false, lockContent{}, err
	}
	var content lockContent
	if err := json.Unmarshal(data, &content); err != nil {
		// corrupt or empty file (treat as stale)
		return true, lockContent{}, nil
	}

	p, err := process.NewProcess(content.PID)
	if err != nil {
		return true, content, nil // process does not exist
	}
	// CreateTime reads /proc/{pid}/stat on Linux (world-readable, always works).
	// On Windows and macOS it can fail for processes owned by a different user,
	// but cloudflared instances sharing a lock file are always running as the
	// same user (the lock directory is derived from ~ via go-homedir).
	ct, err := p.CreateTime()
	if err != nil {
		return true, content, nil // cannot query process (treat as stale)
	}

	diff := ct - content.StartTime
	if diff < 0 {
		diff = -diff
	}
	if diff > startTimeTolerance {
		return true, content, nil // PID was recycled (different process)
	}

	// If the lock file is older than lockTimeout, the auth flow is
	// definitely complete and the process is no longer doing auth work.
	info, err := os.Stat(path)
	if err == nil && time.Since(info.ModTime()) > lockTimeout {
		return true, content, nil
	}

	return false, content, nil // process is alive and actively authenticating
}

func Init(version string) {
	userAgent = fmt.Sprintf("cloudflared/%s", version)
}

// FetchTokenWithRedirect will either load a stored token or generate a new one
// it appends the full url as the redirect URL to the access cli request if opening the browser
func FetchTokenWithRedirect(appURL *url.URL, appInfo *AppInfo, autoClose bool, isFedramp bool, log *zerolog.Logger) (string, error) {
	return getToken(appURL, appInfo, false, autoClose, isFedramp, log)
}

// FetchToken will either load a stored token or generate a new one
// it appends the host of the appURL as the redirect URL to the access cli request if opening the browser
func FetchToken(appURL *url.URL, appInfo *AppInfo, autoClose bool, isFedramp bool, log *zerolog.Logger) (string, error) {
	return getToken(appURL, appInfo, true, autoClose, isFedramp, log)
}

// getToken will either load a stored token or generate a new one
func getToken(appURL *url.URL, appInfo *AppInfo, useHostOnly bool, autoClose bool, isFedramp bool, log *zerolog.Logger) (string, error) {
	if token, err := GetAppTokenIfExists(appInfo); token != "" && err == nil {
		return token, nil
	}

	appTokenPath, err := GenerateAppTokenFilePathFromURL(appInfo.AppDomain, appInfo.AppAUD, keyName)
	if err != nil {
		return "", errors.Wrap(err, "failed to generate app token file path")
	}

	if err = acquireLockFile(appTokenPath, log); err != nil {
		return "", errors.Wrap(err, "failed to acquire app token lock")
	}

	// check to see if another process has gotten a token while we waited for the lock
	if token, err := GetAppTokenIfExists(appInfo); token != "" && err == nil {
		return token, nil
	}

	// If an app token couldn't be found on disk, check for an org token and attempt to exchange it for an app token.
	var orgTokenPath string
	orgToken, err := GetOrgTokenIfExists(appInfo.AuthDomain)
	if err != nil {
		orgTokenPath, err = generateOrgTokenFilePathFromURL(appInfo.AuthDomain)
		if err != nil {
			return "", errors.Wrap(err, "failed to generate org token file path")
		}

		if err = acquireLockFile(orgTokenPath, log); err != nil {
			return "", errors.Wrap(err, "failed to acquire org token lock")
		}
		// check if an org token has been created since the lock was acquired
		orgToken, err = GetOrgTokenIfExists(appInfo.AuthDomain)
	}
	if err == nil {
		if appToken, err := exchangeOrgToken(appURL, orgToken); err != nil {
			log.Debug().Msgf("failed to exchange org token for app token: %s", err)
		} else {
			// generate app path
			if err := os.WriteFile(appTokenPath, []byte(appToken), 0600); err != nil { // nolint: gosec
				return "", errors.Wrap(err, "failed to write app token to disk")
			}
			return appToken, nil
		}
	}
	return getTokensFromEdge(appURL, appInfo.AppAUD, appTokenPath, orgTokenPath, useHostOnly, autoClose, isFedramp, log)
}

// getTokensFromEdge will attempt to use the transfer service to retrieve an app and org token, save them to disk,
// and return the app token.
func getTokensFromEdge(appURL *url.URL, appAUD, appTokenPath, orgTokenPath string, useHostOnly bool, autoClose bool, isFedramp bool, log *zerolog.Logger) (string, error) {
	// If no org token exists or if it couldn't be exchanged for an app token, then run the transfer service flow.

	// this weird parameter is the resource name (token) and the key/value
	// we want to send to the transfer service. the key is token and the value
	// is blank (basically just the id generated in the transfer service)
	resourceData, err := RunTransfer(appURL, appAUD, keyName, keyName, "", true, useHostOnly, autoClose, isFedramp, log, appTokenPath+".url")
	if err != nil {
		return "", errors.Wrap(err, "failed to run transfer service")
	}
	var resp transferServiceResponse
	if err = json.Unmarshal(resourceData, &resp); err != nil {
		return "", errors.Wrap(err, "failed to marshal transfer service response")
	}

	// If we were able to get the auth domain and generate an org token path, lets write it to disk.
	if orgTokenPath != "" {
		if err := os.WriteFile(orgTokenPath, []byte(resp.OrgToken), 0600); err != nil {
			return "", errors.Wrap(err, "failed to write org token to disk")
		}
	}

	if err := os.WriteFile(appTokenPath, []byte(resp.AppToken), 0600); err != nil {
		return "", errors.Wrap(err, "failed to write app token to disk")
	}

	return resp.AppToken, nil
}

// GetAppInfo makes a request to the appURL and stops at the first redirect. The 302 location header will contain the
// auth domain
func GetAppInfo(reqURL *url.URL) (*AppInfo, error) {
	client := &http.Client{
		// do not follow redirects
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// stop after hitting login endpoint since it will contain app path
			if strings.Contains(via[len(via)-1].URL.Path, AccessLoginWorkerPath) {
				return http.ErrUseLastResponse
			}
			return nil
		},
		Timeout: time.Second * 7,
	}

	appInfoReq, err := http.NewRequest("HEAD", reqURL.String(), nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create app info request")
	}
	appInfoReq.Header.Add("User-Agent", userAgent)
	resp, err := client.Do(appInfoReq) // nolint: gosec
	if err != nil {
		return nil, errors.Wrap(err, "failed to get app info")
	}
	_ = resp.Body.Close()

	var aud string
	location := resp.Request.URL
	if strings.Contains(location.Path, AccessLoginWorkerPath) {
		aud = resp.Request.URL.Query().Get("kid")
		if aud == "" {
			return nil, errors.New("Empty app aud")
		}
	} else if audHeader := resp.Header.Get(appAUDHeader); audHeader != "" {
		// 403/401 from the edge will have aud in a header
		aud = audHeader
	} else {
		return nil, fmt.Errorf("failed to find Access application at %s", reqURL.String())
	}

	domain := resp.Header.Get(appDomainHeader)
	if domain == "" {
		return nil, errors.New("Empty app domain")
	}

	return &AppInfo{location.Hostname(), aud, domain}, nil
}

func handleRedirects(req *http.Request, via []*http.Request, orgToken string) error {
	// attach org token to login request
	if strings.Contains(req.URL.Path, AccessLoginWorkerPath) {
		req.AddCookie(&http.Cookie{Name: tokenCookie, Value: orgToken})
	}

	// attach app session cookie to authorized request
	if strings.Contains(req.URL.Path, AccessAuthorizedWorkerPath) {
		// We need to check and see if the CF_APP_SESSION cookie was set
		for _, prevReq := range via {
			if prevReq != nil && prevReq.Response != nil {
				for _, c := range prevReq.Response.Cookies() {
					if c.Name == appSessionCookie {
						req.AddCookie(&http.Cookie{Name: appSessionCookie, Value: c.Value})
						return nil
					}
				}
			}
		}
	}

	// stop after hitting authorized endpoint since it will contain the app token
	if len(via) > 0 && strings.Contains(via[len(via)-1].URL.Path, AccessAuthorizedWorkerPath) {
		return http.ErrUseLastResponse
	}
	return nil
}

// exchangeOrgToken attaches an org token to a request to the appURL and returns an app token. This uses the Access SSO
// flow to automatically generate and return an app token without the login page.
func exchangeOrgToken(appURL *url.URL, orgToken string) (string, error) {
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return handleRedirects(req, via, orgToken)
		},
		Timeout: time.Second * 7,
	}

	appTokenRequest, err := http.NewRequest("HEAD", appURL.String(), nil)
	if err != nil {
		return "", errors.Wrap(err, "failed to create app token request")
	}
	appTokenRequest.Header.Add("User-Agent", userAgent)
	resp, err := client.Do(appTokenRequest) // nolint: gosec
	if err != nil {
		return "", errors.Wrap(err, "failed to get app token")
	}
	_ = resp.Body.Close()
	var appToken string
	for _, c := range resp.Cookies() {
		//if Org token revoked on exchange, getTokensFromEdge instead
		validAppToken := c.Name == tokenCookie && time.Now().Before(c.Expires)
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
	path, err := generateOrgTokenFilePathFromURL(authDomain)
	if err != nil {
		return "", err
	}
	token, err := getTokenIfExists(path)
	if err != nil {
		return "", err
	}
	var payload jwtPayload
	err = json.Unmarshal(token.UnsafePayloadWithoutVerification(), &payload)
	if err != nil {
		return "", err
	}

	if payload.isExpired() {
		err := os.Remove(path)
		return "", err
	}
	return token.CompactSerialize()
}

func GetAppTokenIfExists(appInfo *AppInfo) (string, error) {
	path, err := GenerateAppTokenFilePathFromURL(appInfo.AppDomain, appInfo.AppAUD, keyName)
	if err != nil {
		return "", err
	}
	token, err := getTokenIfExists(path)
	if err != nil {
		return "", err
	}
	var payload jwtPayload
	err = json.Unmarshal(token.UnsafePayloadWithoutVerification(), &payload)
	if err != nil {
		return "", err
	}

	if payload.isExpired() {
		err := os.Remove(path)
		return "", err
	}
	return token.CompactSerialize()
}

// GetTokenIfExists will return the token from local storage if it exists and not expired
func getTokenIfExists(path string) (*jose.JSONWebSignature, error) {
	content, err := os.ReadFile(path) // nolint: gosec
	if err != nil {
		return nil, err
	}
	token, err := jose.ParseSigned(string(content), signatureAlgs)
	if err != nil {
		return nil, err
	}
	return token, nil
}

// RemoveTokenIfExists removes the a token from local storage if it exists
func RemoveTokenIfExists(appInfo *AppInfo) error {
	path, err := GenerateAppTokenFilePathFromURL(appInfo.AppDomain, appInfo.AppAUD, keyName)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}
