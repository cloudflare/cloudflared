package token

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleRedirects_AttachOrgToken(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/cdn-cgi/access/login", nil)
	via := []*http.Request{}
	orgToken := "orgTokenValue"

	_ = handleRedirects(req, via, orgToken)

	// Check if the orgToken cookie is attached
	cookies := req.Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == tokenCookie && cookie.Value == orgToken {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("OrgToken cookie not attached to the request.")
	}
}

func TestHandleRedirects_AttachAppSessionCookie(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/cdn-cgi/access/authorized", nil)
	via := []*http.Request{
		{
			URL: &url.URL{Path: "/cdn-cgi/access/login"},
			Response: &http.Response{
				Header: http.Header{"Set-Cookie": {"CF_AppSession=appSessionValue"}},
			},
		},
	}
	orgToken := "orgTokenValue"

	err := handleRedirects(req, via, orgToken)

	// Check if the appSessionCookie is attached to the request
	cookies := req.Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == appSessionCookie && cookie.Value == "appSessionValue" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("AppSessionCookie not attached to the request.")
	}

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestHandleRedirects_StopAtAuthorizedEndpoint(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com/cdn-cgi/access/authorized", nil)
	via := []*http.Request{
		{
			URL: &url.URL{Path: "other"},
		},
		{
			URL: &url.URL{Path: AccessAuthorizedWorkerPath},
		},
	}
	orgToken := "orgTokenValue"

	err := handleRedirects(req, via, orgToken)

	// Check if ErrUseLastResponse is returned
	if err != http.ErrUseLastResponse {
		t.Errorf("Expected ErrUseLastResponse, got %v", err)
	}
}

func TestJwtPayloadUnmarshal_AudAsString(t *testing.T) {
	jwt := `{"aud":"7afbdaf987054f889b3bdd0d29ebfcd2"}`
	var payload jwtPayload
	if err := json.Unmarshal([]byte(jwt), &payload); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(payload.Aud) != 1 || payload.Aud[0] != "7afbdaf987054f889b3bdd0d29ebfcd2" {
		t.Errorf("Expected aud to be 7afbdaf987054f889b3bdd0d29ebfcd2, got %v", payload.Aud)
	}
}

func TestJwtPayloadUnmarshal_AudAsSlice(t *testing.T) {
	jwt := `{"aud":["7afbdaf987054f889b3bdd0d29ebfcd2", "f835c0016f894768976c01e076844efe"]}`
	var payload jwtPayload
	if err := json.Unmarshal([]byte(jwt), &payload); err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if len(payload.Aud) != 2 || payload.Aud[0] != "7afbdaf987054f889b3bdd0d29ebfcd2" || payload.Aud[1] != "f835c0016f894768976c01e076844efe" {
		t.Errorf("Expected aud to be [7afbdaf987054f889b3bdd0d29ebfcd2, f835c0016f894768976c01e076844efe], got %v", payload.Aud)
	}
}

func TestJwtPayloadUnmarshal_FailsWhenAudIsInt(t *testing.T) {
	jwt := `{"aud":123}`
	var payload jwtPayload
	err := json.Unmarshal([]byte(jwt), &payload)
	wantErr := "aud field is not a string or an array of strings"
	if err.Error() != wantErr {
		t.Errorf("Expected %v, got %v", wantErr, err)
	}
}

func TestJwtPayloadUnmarshal_FailsWhenAudIsArrayOfInts(t *testing.T) {
	jwt := `{"aud": [999, 123] }`
	var payload jwtPayload
	err := json.Unmarshal([]byte(jwt), &payload)
	wantErr := "aud array contains non-string elements"
	if err.Error() != wantErr {
		t.Errorf("Expected %v, got %v", wantErr, err)
	}
}

func TestJwtPayloadUnmarshal_FailsWhenAudIsOmitted(t *testing.T) {
	jwt := `{}`
	var payload jwtPayload
	err := json.Unmarshal([]byte(jwt), &payload)
	wantErr := "aud field is not a string or an array of strings"
	if err.Error() != wantErr {
		t.Errorf("Expected %v, got %v", wantErr, err)
	}
}

// --- Test helpers for metadata JWT tests ---

// testRSAKey generates an RSA key pair for testing.
func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

// signTestMetadataJWT signs a metadataClaims payload with the given RSA key
// and kid, returning a compact JWS string.
func signTestMetadataJWT(t *testing.T, claims *metadataClaims, key *rsa.PrivateKey, kid string) string {
	return signTestJWT(t, claims, key, kid)
}

func signTestJWT(t *testing.T, claims any, key *rsa.PrivateKey, kid string) string {
	t.Helper()
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), kid),
	)
	require.NoError(t, err)

	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	jws, err := signer.Sign(payload)
	require.NoError(t, err)

	compact, err := jws.CompactSerialize()
	require.NoError(t, err)
	return compact
}

func useTempHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	homedir.Reset()
	t.Cleanup(homedir.Reset)
}

func testAuthDomain(t *testing.T, authDomain string) url.URL {
	t.Helper()
	parsed, err := parseAuthDomain(authDomain)
	require.NoError(t, err)
	return parsed
}

// testJWKS builds a JSONWebKeySet containing the public key of the given
// RSA key pair.
func testJWKS(t *testing.T, key *rsa.PrivateKey, kid string) *jose.JSONWebKeySet {
	t.Helper()
	return &jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: kid, Algorithm: string(jose.RS256), Use: "sig"},
		},
	}
}

// --- Metadata JWT verification tests ---

func TestParseAuthDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		authDomain   string
		wantHostname string
		wantErr      bool
	}{
		{name: "valid retail", authDomain: "myteam.cloudflareaccess.com", wantHostname: "myteam.cloudflareaccess.com"},
		{name: "valid fedramp", authDomain: "myteam.fed.cloudflareaccess.com", wantHostname: "myteam.fed.cloudflareaccess.com"},
		{name: "invalid domain", authDomain: "evil.com", wantErr: true},
		{name: "suffix without dot", authDomain: "evilcloudflareaccess.com", wantErr: true},
		{name: "empty", authDomain: "", wantErr: true},
		{name: "fragment injection", authDomain: "keys.evil.example#.cloudflareaccess.com", wantErr: true},
		{name: "path injection", authDomain: "keys.evil.example/x.cloudflareaccess.com", wantErr: true},
		{name: "query injection", authDomain: "keys.evil.example?x=.cloudflareaccess.com", wantErr: true},
		{name: "userinfo injection", authDomain: "team.cloudflareaccess.com@evil.example", wantErr: true},
		{name: "legitimate host with fragment", authDomain: "team.cloudflareaccess.com#fragment", wantHostname: "team.cloudflareaccess.com"},
		{name: "legitimate host with path", authDomain: "team.cloudflareaccess.com/path", wantHostname: "team.cloudflareaccess.com"},
		{name: "userinfo discarded", authDomain: "evil.example@team.cloudflareaccess.com", wantHostname: "team.cloudflareaccess.com"},
		{name: "port discarded", authDomain: "team.cloudflareaccess.com:443", wantHostname: "team.cloudflareaccess.com"},
		{name: "uppercase normalized", authDomain: "TEAM.CLOUDFLAREACCESS.COM", wantHostname: "team.cloudflareaccess.com"},
		{name: "malformed URL", authDomain: "%", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			authDomain, err := parseAuthDomain(tc.authDomain)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "https", authDomain.Scheme)
				assert.Equal(t, tc.wantHostname, authDomain.Hostname())
				assert.Empty(t, authDomain.Path)
				assert.Empty(t, authDomain.RawQuery)
				assert.Empty(t, authDomain.Fragment)
				assert.Nil(t, authDomain.User)
			}
		})
	}
}

func TestFetchMetadataJWT_ReturnsAppInfoErrorOnError(t *testing.T) {
	t.Parallel()

	_, err := fetchMetadataJWT("://invalid")
	require.ErrorContains(t, err, "failed to create app info request")

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	serverURL := server.URL
	server.Close()

	_, err = fetchMetadataJWT(serverURL)
	require.ErrorContains(t, err, "failed to get app info")
}

func TestValidateMetadataIssuedAt(t *testing.T) {
	t.Parallel()

	now := time.Unix(2_000_000_000, 0)
	tests := []struct {
		name    string
		iat     int64
		wantErr bool
	}{
		{name: "current", iat: now.Unix()},
		{name: "maximum age", iat: now.Add(-metadataMaxAge).Unix()},
		{name: "maximum future skew", iat: now.Add(metadataAllowedClockSkew).Unix()},
		{name: "too old", iat: now.Add(-metadataMaxAge - time.Second).Unix(), wantErr: true},
		{name: "too far in future", iat: now.Add(metadataAllowedClockSkew + time.Second).Unix(), wantErr: true},
		{name: "missing", iat: 0, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateMetadataIssuedAt(tc.iat, now)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestVerifyMetadataJWT_ValidSignature(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	kid := "test-key-1"
	keySet := testJWKS(t, key, kid)

	claims := &metadataClaims{
		Type:        "match",
		Hostname:    "app.example.com",
		AuthDomain:  "myteam.cloudflareaccess.com",
		AUD:         "test-aud",
		AppHostname: "app.example.com",
		IAT:         1234567890,
	}
	rawJWT := signTestMetadataJWT(t, claims, key, kid)

	verified, err := verifyMetadataJWT(rawJWT, keySet)
	require.NoError(t, err)
	assert.Equal(t, "app.example.com", verified.Hostname)
	assert.Equal(t, "myteam.cloudflareaccess.com", verified.AuthDomain)
	assert.Equal(t, "test-aud", verified.AUD)
	assert.Equal(t, "app.example.com", verified.AppHostname)
}

func TestVerifyMetadataJWT_InvalidSignature(t *testing.T) {
	t.Parallel()

	signingKey := testRSAKey(t)
	wrongKey := testRSAKey(t)
	kid := "test-key-1"
	keySet := testJWKS(t, wrongKey, kid) // JWKS has wrong public key

	claims := &metadataClaims{
		Type:       "match",
		Hostname:   "app.example.com",
		AuthDomain: "myteam.cloudflareaccess.com",
		AUD:        "test-aud",
	}
	rawJWT := signTestMetadataJWT(t, claims, signingKey, kid)

	_, err := verifyMetadataJWT(rawJWT, keySet)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify metadata JWT signature")
}

func TestVerifyMetadataJWT_KidMismatch(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	keySet := testJWKS(t, key, "key-1")

	claims := &metadataClaims{
		Type:       "match",
		Hostname:   "app.example.com",
		AuthDomain: "myteam.cloudflareaccess.com",
		AUD:        "test-aud",
	}
	rawJWT := signTestMetadataJWT(t, claims, key, "key-2") // different kid

	_, err := verifyMetadataJWT(rawJWT, keySet)
	require.Error(t, err)
}

func TestGetAppInfo_RejectsNoMetadataHeader(t *testing.T) {
	t.Parallel()

	// Server that returns no cf-access-metadata header.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reqURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	appInfo, err := GetAppInfo(reqURL)
	assert.Nil(t, appInfo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find Access application")
}

func TestGetAppInfo_RejectsNonCloudflareAuthDomain(t *testing.T) {
	t.Parallel()

	key := testRSAKey(t)
	kid := "test-key-1"

	claims := &metadataClaims{
		Type:       "match",
		Hostname:   "app.evil.com",
		AuthDomain: "evil.com",
		AUD:        "fake-aud",
	}
	rawJWT := signTestMetadataJWT(t, claims, key, kid)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(accessMetadataRespHeader, rawJWT)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reqURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	appInfo, err := GetAppInfo(reqURL)
	assert.Nil(t, appInfo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_domain validation failed")
}

func TestGetAppInfo_RejectsHostnameMismatch(t *testing.T) {
	useTempHome(t)

	key := testRSAKey(t)
	kid := "test-key-1"
	keySet := testJWKS(t, key, kid)
	issuedAt := time.Now().Add(-time.Hour)
	authDomain := "hostname-mismatch.cloudflareaccess.com"
	require.NoError(t, writeJWKSCache(testAuthDomain(t, authDomain), keySet))

	claims := &metadataClaims{
		Type:        "match",
		Hostname:    "legit-app.example.com",
		AuthDomain:  authDomain,
		AUD:         "test-aud",
		AppHostname: "legit-app.example.com",
		IAT:         issuedAt.Unix(),
	}
	rawJWT := signTestMetadataJWT(t, claims, key, kid)

	metadataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(accessMetadataRespHeader, rawJWT)
		w.WriteHeader(http.StatusOK)
	}))
	defer metadataServer.Close()
	reqURL, err := url.Parse(metadataServer.URL)
	require.NoError(t, err)

	appInfo, err := GetAppInfo(reqURL)
	assert.Nil(t, appInfo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match request host")
}

func TestGetAppInfo_AcceptsValidMetadata(t *testing.T) {
	useTempHome(t)

	key := testRSAKey(t)
	kid := "test-key-1"
	keySet := testJWKS(t, key, kid)
	issuedAt := time.Now().Add(-time.Hour)
	authDomain := "valid-metadata.cloudflareaccess.com"
	require.NoError(t, writeJWKSCache(testAuthDomain(t, authDomain), keySet))

	var rawJWT string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(accessMetadataReqHeader) == "true" {
			w.Header().Set(accessMetadataRespHeader, rawJWT)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reqURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	claims := &metadataClaims{
		Type:        "match",
		Hostname:    reqURL.Hostname(),
		AuthDomain:  authDomain,
		AUD:         "test-aud",
		AppHostname: "*.example.com",
		IAT:         issuedAt.Unix(),
	}
	rawJWT = signTestMetadataJWT(t, claims, key, kid)

	appInfo, err := GetAppInfo(reqURL)
	require.NoError(t, err)
	assert.Equal(t, authDomain, appInfo.AuthDomain)
	assert.Equal(t, "test-aud", appInfo.AppAUD)
	assert.Equal(t, "*.example.com", appInfo.AppHostname)

	claims.AppHostname = ""
	rawJWT = signTestMetadataJWT(t, claims, key, kid)
	appInfo, err = GetAppInfo(reqURL)
	require.NoError(t, err)
	assert.Equal(t, claims.Hostname, appInfo.AppHostname)
	tokenPath, err := GenerateAppTokenFilePathFromURL(appInfo.AppHostname, appInfo.AppAUD, keyName)
	require.NoError(t, err)
	assert.Equal(t, claims.Hostname+"-test-aud-token", filepath.Base(tokenPath))
}

func TestGetAppInfo_RejectsInvalidClaims(t *testing.T) {
	useTempHome(t)

	key := testRSAKey(t)
	kid := "test-key-1"
	keySet := testJWKS(t, key, kid)
	issuedAt := time.Now().Add(-time.Hour)
	authDomain := "invalid-claims.cloudflareaccess.com"
	require.NoError(t, writeJWKSCache(testAuthDomain(t, authDomain), keySet))

	tests := []struct {
		name   string
		mutate func(*metadataClaims)
	}{
		{
			name: "non-match type",
			mutate: func(claims *metadataClaims) {
				claims.Type = "no_match"
			},
		},
		{
			name: "empty audience",
			mutate: func(claims *metadataClaims) {
				claims.AUD = ""
			},
		},
		{
			name: "stale issued-at",
			mutate: func(claims *metadataClaims) {
				claims.IAT = issuedAt.Add(-metadataMaxAge).Unix()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rawJWT string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(accessMetadataRespHeader, rawJWT)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			reqURL, err := url.Parse(server.URL)
			require.NoError(t, err)
			claims := &metadataClaims{
				Type:        "match",
				Hostname:    reqURL.Hostname(),
				AuthDomain:  authDomain,
				AUD:         "test-aud",
				AppHostname: "app.example.com",
				IAT:         issuedAt.Unix(),
			}
			tc.mutate(claims)
			rawJWT = signTestMetadataJWT(t, claims, key, kid)

			appInfo, err := GetAppInfo(reqURL)
			assert.Nil(t, appInfo)
			assert.Error(t, err)
		})
	}
}

func TestFetchJWKS(t *testing.T) {
	t.Parallel()

	keySetJSON, err := json.Marshal(testJWKS(t, testRSAKey(t), "test-key"))
	require.NoError(t, err)
	tests := []struct {
		name        string
		status      int
		body        []byte
		wantErrText string
	}{
		{name: "valid response", status: http.StatusOK, body: keySetJSON},
		{name: "non-OK response", status: http.StatusServiceUnavailable, wantErrText: "returned status 503"},
		{name: "malformed response", status: http.StatusOK, body: []byte("not-json"), wantErrText: "failed to parse JWKS"},
		{name: "oversized response", status: http.StatusOK, body: make([]byte, maxJWKSResponseSize+1), wantErrText: "JWKS response body exceeds"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requestedPath := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestedPath <- r.URL.Path
				w.WriteHeader(tc.status)
				_, _ = w.Write(tc.body)
			}))
			defer server.Close()

			authDomain, err := url.Parse(server.URL)
			require.NoError(t, err)
			keySet, err := fetchJWKS(*authDomain)
			assert.Equal(t, accessCertPath, <-requestedPath)
			if tc.wantErrText != "" {
				require.ErrorContains(t, err, tc.wantErrText)
				assert.Nil(t, keySet)
				return
			}
			require.NoError(t, err)
			require.Len(t, keySet.Keys, 1)
			assert.Equal(t, "test-key", keySet.Keys[0].KeyID)
		})
	}
}

func TestFetchJWKS_DoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var redirectTargetRequests atomic.Int32
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectTargetRequests.Add(1)
	}))
	defer redirectTarget.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", redirectTarget.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	authDomain, err := url.Parse(server.URL)
	require.NoError(t, err)

	keySet, err := fetchJWKS(*authDomain)
	require.ErrorContains(t, err, "returned status 302")
	assert.Nil(t, keySet)
	assert.Zero(t, redirectTargetRequests.Load())
}

func TestVerifyMetadataWithRetry_RefreshesCachedJWKS(t *testing.T) {
	useTempHome(t)

	staleKey := testRSAKey(t)
	currentKey := testRSAKey(t)
	kid := "current-key"
	currentKeySet := testJWKS(t, currentKey, kid)
	currentKeySetJSON, err := json.Marshal(currentKeySet)
	require.NoError(t, err)

	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		assert.Equal(t, accessCertPath, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(currentKeySetJSON)
	}))
	defer server.Close()
	authDomain, err := url.Parse(server.URL)
	require.NoError(t, err)
	require.NoError(t, writeJWKSCache(*authDomain, testJWKS(t, staleKey, "stale-key")))
	cachePath, err := jwksCachePath(*authDomain)
	require.NoError(t, err)
	refreshAllowedAt := time.Now().Add(-jwksMinRefreshInterval - time.Second)
	require.NoError(t, os.Chtimes(cachePath, refreshAllowedAt, refreshAllowedAt))

	claims := &metadataClaims{
		Type:       "match",
		Hostname:   "app.example.com",
		AuthDomain: "team.cloudflareaccess.com",
		AUD:        "test-aud",
		IAT:        time.Now().Unix(),
	}
	rawJWT := signTestMetadataJWT(t, claims, currentKey, kid)

	verified, err := verifyMetadataWithRetry(rawJWT, *authDomain)
	require.NoError(t, err)
	assert.Equal(t, claims, verified)
	assert.Equal(t, int32(1), fetches.Load())

	cached, _, err := getCachedJWKS(*authDomain)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.Len(t, cached.Keys, 1)
	assert.Equal(t, kid, cached.Keys[0].KeyID)
}

func TestVerifyMetadataWithRetry_DoesNotRefreshRecentCachedJWKS(t *testing.T) {
	useTempHome(t)

	staleKey := testRSAKey(t)
	currentKey := testRSAKey(t)
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		fetches.Add(1)
	}))
	defer server.Close()
	authDomain, err := url.Parse(server.URL)
	require.NoError(t, err)
	require.NoError(t, writeJWKSCache(*authDomain, testJWKS(t, staleKey, "stale-key")))

	claims := &metadataClaims{
		Type:       "match",
		Hostname:   "app.example.com",
		AuthDomain: "team.cloudflareaccess.com",
		AUD:        "test-aud",
		IAT:        time.Now().Unix(),
	}
	rawJWT := signTestMetadataJWT(t, claims, currentKey, "current-key")

	verified, err := verifyMetadataWithRetry(rawJWT, *authDomain)
	assert.Nil(t, verified)
	require.ErrorContains(t, err, "failed to verify metadata JWT signature")
	assert.Zero(t, fetches.Load())
}

func TestGetJWKSWithCache_RefreshesExpiredCache(t *testing.T) {
	useTempHome(t)

	staleKey := testRSAKey(t)
	currentKey := testRSAKey(t)
	currentKeySet := testJWKS(t, currentKey, "current-key")
	currentKeySetJSON, err := json.Marshal(currentKeySet)
	require.NoError(t, err)

	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, _ = w.Write(currentKeySetJSON)
	}))
	defer server.Close()
	authDomain, err := url.Parse(server.URL)
	require.NoError(t, err)
	require.NoError(t, writeJWKSCache(*authDomain, testJWKS(t, staleKey, "stale-key")))
	cachePath, err := jwksCachePath(*authDomain)
	require.NoError(t, err)
	expiredAt := time.Now().Add(-jwksCacheTTL - time.Second)
	require.NoError(t, os.Chtimes(cachePath, expiredAt, expiredAt))

	keySet, cachedAt, err := getJWKSWithCache(*authDomain)
	require.NoError(t, err)
	assert.True(t, cachedAt.IsZero())
	require.Len(t, keySet.Keys, 1)
	assert.Equal(t, "current-key", keySet.Keys[0].KeyID)
	assert.Equal(t, int32(1), fetches.Load())
}

func TestJWKSCache_ReturnsReadAndWriteErrors(t *testing.T) {
	useTempHome(t)

	authDomain := testAuthDomain(t, "cache-errors.cloudflareaccess.com")
	cachePath, err := jwksCachePath(authDomain)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cachePath, []byte("not-json"), 0600))

	keySet, _, err := getCachedJWKS(authDomain)
	assert.Nil(t, keySet)
	require.ErrorContains(t, err, "failed to parse JWKS cache")

	require.NoError(t, os.Remove(cachePath))
	require.NoError(t, os.Mkdir(cachePath, 0700))
	err = writeJWKSCache(authDomain, testJWKS(t, testRSAKey(t), "test-key"))
	require.ErrorContains(t, err, "failed to replace JWKS cache")
}

func TestGetJWKSWithCache_FallsBackOnCacheErrors(t *testing.T) {
	useTempHome(t)

	keySet := testJWKS(t, testRSAKey(t), "test-key")
	keySetJSON, err := json.Marshal(keySet)
	require.NoError(t, err)
	var fetches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, _ = w.Write(keySetJSON)
	}))
	defer server.Close()
	authDomain, err := url.Parse(server.URL)
	require.NoError(t, err)
	cachePath, err := jwksCachePath(*authDomain)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(cachePath, []byte("not-json"), 0600))
	fetched, _, err := getJWKSWithCache(*authDomain)
	require.NoError(t, err)
	require.Len(t, fetched.Keys, 1)
	assert.Equal(t, "test-key", fetched.Keys[0].KeyID)

	require.NoError(t, os.Remove(cachePath))
	require.NoError(t, os.Mkdir(cachePath, 0700))
	fetched, _, err = getJWKSWithCache(*authDomain)
	require.NoError(t, err)
	require.Len(t, fetched.Keys, 1)
	assert.Equal(t, "test-key", fetched.Keys[0].KeyID)
	assert.Equal(t, int32(2), fetches.Load())
}

func TestJWKSCache_WriteAndRead(t *testing.T) {
	useTempHome(t)

	key := testRSAKey(t)
	kid := "cache-test-key"
	keySet := testJWKS(t, key, kid)

	authDomain := testAuthDomain(t, "test-cache.cloudflareaccess.com")

	require.NoError(t, writeJWKSCache(authDomain, keySet))

	cached, _, err := getCachedJWKS(authDomain)
	require.NoError(t, err)
	require.NotNil(t, cached)
	assert.Len(t, cached.Keys, 1)
	assert.Equal(t, kid, cached.Keys[0].KeyID)

	replacementKey := testRSAKey(t)
	replacementKid := "replacement-key"
	require.NoError(t, writeJWKSCache(authDomain, testJWKS(t, replacementKey, replacementKid)))
	cached, _, err = getCachedJWKS(authDomain)
	require.NoError(t, err)
	require.NotNil(t, cached)
	assert.Len(t, cached.Keys, 1)
	assert.Equal(t, replacementKid, cached.Keys[0].KeyID)

	configPath, err := getConfigPath()
	require.NoError(t, err)
	entries, err := os.ReadDir(configPath)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), ".tmp")
	}
}

func TestGetAppTokenIfExists_ValidatesAudience(t *testing.T) {
	useTempHome(t)

	key := testRSAKey(t)
	appInfo := &AppInfo{AppAUD: "expected-aud", AppHostname: "app.example.com"}
	path, err := GenerateAppTokenFilePathFromURL(appInfo.AppHostname, appInfo.AppAUD, keyName)
	require.NoError(t, err)

	wrongAudienceToken := signTestJWT(t, map[string]any{
		"aud": "different-aud",
		"exp": time.Now().Add(time.Hour).Unix(),
	}, key, "token-key")
	require.NoError(t, os.WriteFile(path, []byte(wrongAudienceToken), 0600))

	token, err := GetAppTokenIfExists(appInfo)
	assert.Empty(t, token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not include expected application audience")
	assert.NoFileExists(t, path)

	matchingToken := signTestJWT(t, map[string]any{
		"aud": []string{"other-aud", appInfo.AppAUD},
		"exp": time.Now().Add(time.Hour).Unix(),
	}, key, "token-key")
	require.NoError(t, os.WriteFile(path, []byte(matchingToken), 0600))

	token, err = GetAppTokenIfExists(appInfo)
	require.NoError(t, err)
	assert.Equal(t, matchingToken, token)
}
