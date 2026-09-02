package token

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
