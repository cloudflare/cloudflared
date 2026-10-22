package token

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
)

func TestFetchTokenWithRedirectAllowsSameProcessReauthentication(t *testing.T) {
	useTempHome(t)

	log := zerolog.Nop()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	appToken := signedAccessToken(t, key, "app-aud", time.Now().Add(time.Hour))
	orgToken := signedAccessToken(t, key, "org-aud", time.Now().Add(time.Hour))

	appInfo := &AppInfo{
		AuthDomain:  "auth.example.com",
		AppAUD:      "app-aud",
		AppHostname: "app.example.com",
	}
	appTokenPath, err := GenerateAppTokenFilePathFromURL(appInfo.AppHostname, appInfo.AppAUD, keyName)
	require.NoError(t, err)

	orgTokenPath, err := generateOrgTokenFilePathFromURL(appInfo.AuthDomain)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(orgTokenPath, []byte(orgToken), 0600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     tokenCookie,
			Value:    appToken,
			Expires:  time.Now().Add(time.Hour),
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	appURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	fetchedToken, err := FetchTokenWithRedirect(appURL, appInfo, false, false, &log)
	require.NoError(t, err)
	assert.Equal(t, appToken, fetchedToken)
	assert.NoFileExists(t, appTokenPath+".lock")

	// This mirrors the WebSocket 302 retry path, which removes the app token
	// before fetching a replacement in the same cloudflared process.
	require.NoError(t, RemoveTokenIfExists(appInfo))

	done := make(chan struct{})
	var retryToken string
	var retryErr error
	go func() {
		defer close(done)
		retryToken, retryErr = FetchTokenWithRedirect(appURL, appInfo, false, false, &log)
	}()

	select {
	case <-done:
		require.NoError(t, retryErr)
		assert.Equal(t, appToken, retryToken)
		assert.NoFileExists(t, appTokenPath+".lock")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("same-process token retry waited on a lock left behind by the first token fetch")
	}
}

// relayRecorder stands in for the token transfer relay. It records every
// polled URL and answers with the app and org tokens encrypted to the
// cloudflared public key embedded in the polling path.
type relayRecorder struct {
	appToken string
	orgToken string

	mu   sync.Mutex
	urls []string
}

func (r *relayRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()

	recipientKey, ok := strings.CutPrefix(req.URL.Path, "/transfer/")
	if !ok {
		return nil, fmt.Errorf("unexpected relay path %q", req.URL.Path)
	}
	sender, err := NewEncrypter()
	if err != nil {
		return nil, err
	}
	recipient, err := sender.decodePublicKey(recipientKey)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(transferServiceResponse{AppToken: r.appToken, OrgToken: r.orgToken})
	if err != nil {
		return nil, err
	}
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	sealed := box.Seal(nonce[:], payload, &nonce, recipient, sender.privateKey)

	header := make(http.Header)
	header.Set("service-public-key", sender.PublicKey())
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(base64.StdEncoding.EncodeToString(sealed))),
		Request:    req,
	}, nil
}

func (r *relayRecorder) polledURLs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.urls)
}

// stubTransfer replaces the browser launcher and relay transport for the
// duration of the test so no browser is opened and no production relay is
// contacted.
func stubTransfer(t *testing.T, relay *relayRecorder) {
	t.Helper()
	origOpenBrowser, origTransport := openBrowser, transferTransport
	openBrowser = func(string) error { return nil }
	transferTransport = relay
	t.Cleanup(func() {
		openBrowser, transferTransport = origOpenBrowser, origTransport
	})
}

// discoverTestApp serves a signed metadata JWT for authDomain from a local
// server and returns the app URL with the AppInfo verified by GetAppInfo.
func discoverTestApp(t *testing.T, authDomain, appHostname string) (*url.URL, *AppInfo) {
	t.Helper()

	key := testRSAKey(t)
	kid := "test-key-1"
	require.NoError(t, writeJWKSCache(testAuthDomain(t, authDomain), testJWKS(t, key, kid)))

	var rawJWT string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(accessMetadataReqHeader) == accessMetadataReqValue {
			w.Header().Set(accessMetadataRespHeader, rawJWT)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	appURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	rawJWT = signTestMetadataJWT(t, &metadataClaims{
		Type:        metadataMatchType,
		Hostname:    appURL.Hostname(),
		AuthDomain:  authDomain,
		AUD:         "app-aud",
		AppHostname: appHostname,
		IAT:         time.Now().Unix(),
	}, key, kid)

	appInfo, err := GetAppInfo(appURL)
	require.NoError(t, err)
	return appURL, appInfo
}

func TestFetchTokenSelectsTransferRelay(t *testing.T) {
	type fetchFunc func(*url.URL, *AppInfo, bool, bool, *zerolog.Logger) (string, error)

	tests := []struct {
		name         string
		authDomain   string
		appHostname  string
		forceFedramp bool
		fetch        fetchFunc
		wantRelay    string
	}{
		{
			name:       "fed auth domain without flag",
			authDomain: "team.fed.cloudflareaccess.com",
			fetch:      FetchTokenWithRedirect,
			wantRelay:  fedStoreURL,
		},
		{
			name:       "fed auth domain without flag via FetchToken",
			authDomain: "team.fed.cloudflareaccess.com",
			fetch:      FetchToken,
			wantRelay:  fedStoreURL,
		},
		{
			name:       "fed auth domain normalized",
			authDomain: "Team.FED.CloudflareAccess.com:443",
			fetch:      FetchTokenWithRedirect,
			wantRelay:  fedStoreURL,
		},
		{
			name:       "retail auth domain without flag",
			authDomain: "team.cloudflareaccess.com",
			fetch:      FetchTokenWithRedirect,
			wantRelay:  baseStoreURL,
		},
		{
			name:       "retail auth domain via FetchToken",
			authDomain: "team.cloudflareaccess.com",
			fetch:      FetchToken,
			wantRelay:  baseStoreURL,
		},
		{
			name:         "retail auth domain with flag",
			authDomain:   "team.cloudflareaccess.com",
			forceFedramp: true,
			fetch:        FetchTokenWithRedirect,
			wantRelay:    fedStoreURL,
		},
		{
			name:         "fed auth domain with flag",
			authDomain:   "team.fed.cloudflareaccess.com",
			forceFedramp: true,
			fetch:        FetchToken,
			wantRelay:    fedStoreURL,
		},
		{
			name:        "retail auth domain with fed-looking app hostname",
			authDomain:  "team.cloudflareaccess.com",
			appHostname: "app.fed.cloudflareaccess.com",
			fetch:       FetchTokenWithRedirect,
			wantRelay:   baseStoreURL,
		},
		{
			name:       "retail team named fed",
			authDomain: "fed.cloudflareaccess.com",
			fetch:      FetchTokenWithRedirect,
			wantRelay:  baseStoreURL,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			useTempHome(t)
			log := zerolog.Nop()
			key := testRSAKey(t)

			relay := &relayRecorder{
				appToken: signedAccessToken(t, key, "app-aud", time.Now().Add(time.Hour)),
				orgToken: signedAccessToken(t, key, "org-aud", time.Now().Add(time.Hour)),
			}
			stubTransfer(t, relay)

			appURL, appInfo := discoverTestApp(t, tc.authDomain, tc.appHostname)

			fetchedToken, err := tc.fetch(appURL, appInfo, false, tc.forceFedramp, &log)
			require.NoError(t, err)
			assert.Equal(t, relay.appToken, fetchedToken)

			polled := relay.polledURLs()
			require.Len(t, polled, 1)
			assert.True(t, strings.HasPrefix(polled[0], tc.wantRelay+"transfer/"),
				"polled %q, want relay %q", polled[0], tc.wantRelay)

			cachedApp, err := GetAppTokenIfExists(appInfo)
			require.NoError(t, err)
			assert.Equal(t, relay.appToken, cachedApp)
			cachedOrg, err := GetOrgTokenIfExists(appInfo.AuthDomain)
			require.NoError(t, err)
			assert.Equal(t, relay.orgToken, cachedOrg)
		})
	}
}

func TestFetchTokenFedAuthDomainSkipsRelayWhenTokensCached(t *testing.T) {
	useTempHome(t)
	log := zerolog.Nop()
	key := testRSAKey(t)

	relay := &relayRecorder{}
	stubTransfer(t, relay)

	_, appInfo := discoverTestApp(t, "team.fed.cloudflareaccess.com", "")
	appToken := signedAccessToken(t, key, "app-aud", time.Now().Add(time.Hour))
	orgToken := signedAccessToken(t, key, "org-aud", time.Now().Add(time.Hour))

	// Org token exchange: the app server returns an app token for the org token.
	orgTokenPath, err := generateOrgTokenFilePathFromURL(appInfo.AuthDomain)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(orgTokenPath, []byte(orgToken), 0600))
	exchangeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     tokenCookie,
			Value:    appToken,
			Expires:  time.Now().Add(time.Hour),
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(exchangeServer.Close)
	exchangeURL, err := url.Parse(exchangeServer.URL)
	require.NoError(t, err)

	fetchedToken, err := FetchTokenWithRedirect(exchangeURL, appInfo, false, false, &log)
	require.NoError(t, err)
	assert.Equal(t, appToken, fetchedToken)

	// Cached app token: returned directly from disk.
	fetchedToken, err = FetchToken(exchangeURL, appInfo, false, false, &log)
	require.NoError(t, err)
	assert.Equal(t, appToken, fetchedToken)

	assert.Empty(t, relay.polledURLs())
}

func signedAccessToken(t *testing.T, key *rsa.PrivateKey, aud string, expiresAt time.Time) string {
	t.Helper()

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	require.NoError(t, err)

	payload, err := json.Marshal(map[string]any{
		"aud": aud,
		"exp": expiresAt.Unix(),
	})
	require.NoError(t, err)

	jws, err := signer.Sign(payload)
	require.NoError(t, err)

	token, err := jws.CompactSerialize()
	require.NoError(t, err)
	return token
}
