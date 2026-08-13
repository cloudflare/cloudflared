package token

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

const (
	accessCertPath         = "/cdn-cgi/access/certs"
	accessDomainSuffix     = ".cloudflareaccess.com"
	httpsScheme            = "https"
	jwksCacheSuffix        = "-jwks"
	maxJWKSResponseSize    = 1 << 20
	jwksCacheTTL           = 24 * time.Hour
	jwksMinRefreshInterval = time.Minute
)

var signatureAlgs = []jose.SignatureAlgorithm{jose.RS256}

// metadataClaims represents the claims in the signed metadata JWT returned
// by the Cloudflare Access edge when CF-Access-Metadata-Request: true is set.
type metadataClaims struct {
	Type       string `json:"type"`
	Hostname   string `json:"hostname"`
	AuthDomain string `json:"auth_domain"`
	AUD        string `json:"aud"`
	// This is the hostname as defined in the Access application, including wildcards.
	AppHostname string `json:"app_hostname"`
	IAT         int64  `json:"iat"`
}

// decodeMetadataUnverified decodes the JWT payload without verifying the
// signature.
func decodeMetadataUnverified(rawJWT string) (*metadataClaims, error) {
	jws, err := jose.ParseSigned(rawJWT, signatureAlgs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse metadata JWT")
	}

	payload := jws.UnsafePayloadWithoutVerification()
	var claims metadataClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.Wrap(err, "failed to decode metadata JWT claims")
	}
	return &claims, nil
}

// verifyMetadataJWT verifies the metadata JWT signature against the provided
// JWKS and returns the decoded claims.
func verifyMetadataJWT(rawJWT string, keySet *jose.JSONWebKeySet) (*metadataClaims, error) {
	jws, err := jose.ParseSigned(rawJWT, signatureAlgs)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse metadata JWT")
	}

	payload, err := jws.Verify(keySet)
	if err != nil {
		return nil, errors.Wrap(err, "failed to verify metadata JWT signature")
	}

	var claims metadataClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.Wrap(err, "failed to decode verified metadata JWT claims")
	}
	return &claims, nil
}

// parseAuthDomain extracts the canonical hostname used for JWKS requests and
// cache paths from the auth_domain claim.
func parseAuthDomain(authDomain string) (url.URL, error) {
	parsed, err := url.Parse(httpsScheme + "://" + authDomain)
	if err != nil {
		return url.URL{}, fmt.Errorf("failed to parse auth_domain %q: %w", authDomain, err)
	}
	hostname := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(hostname, accessDomainSuffix) {
		return url.URL{}, fmt.Errorf("auth_domain %q does not end with %q", authDomain, accessDomainSuffix)
	}
	return url.URL{Scheme: httpsScheme, Host: hostname}, nil
}

// fetchJWKS fetches the JWKS from the auth domain's certs endpoint over HTTPS.
func fetchJWKS(authDomain url.URL) (*jose.JSONWebKeySet, error) {
	jwksURL := authDomain
	jwksURL.Path = accessCertPath

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: time.Second * 10,
	}
	resp, err := client.Get(jwksURL.String()) // nolint: gosec
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch JWKS from %s", jwksURL.String())
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint %s returned status %d", jwksURL.String(), resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSResponseSize+1))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read JWKS response body")
	}
	if len(body) > maxJWKSResponseSize {
		return nil, fmt.Errorf("JWKS response body exceeds %d bytes", maxJWKSResponseSize)
	}

	var keySet jose.JSONWebKeySet
	if err := json.Unmarshal(body, &keySet); err != nil {
		return nil, errors.Wrap(err, "failed to parse JWKS")
	}
	return &keySet, nil
}

// jwksCachePath returns the on-disk path for cached JWKS for the given auth domain.
func jwksCachePath(authDomain url.URL) (string, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return "", err
	}
	name := authDomain.Hostname() + jwksCacheSuffix
	return filepath.Join(configPath, name), nil
}

// getCachedJWKS loads JWKS and its modification time from the disk cache.
// A missing cache file is returned as a cache miss without an error.
func getCachedJWKS(authDomain url.URL) (*jose.JSONWebKeySet, time.Time, error) {
	path, err := jwksCachePath(authDomain)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to determine JWKS cache path: %w", err)
	}
	file, err := os.Open(path) // nolint: gosec
	if os.IsNotExist(err) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to open JWKS cache %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to stat JWKS cache %s: %w", path, err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to read JWKS cache %s: %w", path, err)
	}
	var keySet jose.JSONWebKeySet
	if err := json.Unmarshal(data, &keySet); err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to parse JWKS cache %s: %w", path, err)
	}
	return &keySet, info.ModTime(), nil
}

// writeJWKSCache writes the JWKS to the disk cache atomically. It writes to
// a temporary file first and then renames, so concurrent readers never see a
// partial file.
func writeJWKSCache(authDomain url.URL, keySet *jose.JSONWebKeySet) error {
	path, err := jwksCachePath(authDomain)
	if err != nil {
		return fmt.Errorf("failed to determine JWKS cache path: %w", err)
	}
	data, err := json.Marshal(keySet)
	if err != nil {
		return fmt.Errorf("failed to marshal JWKS cache: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".jwks-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary JWKS cache file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	// Close explicitly before cleanup or rename because Windows cannot remove or rename an open file.
	if _, err := tmp.Write(data); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return fmt.Errorf("failed to write JWKS cache (%v) and close temporary file: %w", err, closeErr)
		}
		return fmt.Errorf("failed to write JWKS cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary JWKS cache file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to replace JWKS cache %s: %w", path, err)
	}
	return nil
}

// getJWKSWithCache returns a TTL-fresh cache entry or fetches fresh keys. Cache
// errors are logged and degraded; a zero cachedAt means the keys were fetched
// during this call and must not trigger an immediate verification retry.
func getJWKSWithCache(authDomain url.URL) (*jose.JSONWebKeySet, time.Time, error) {
	cached, cachedAt, cacheErr := getCachedJWKS(authDomain)
	if cacheErr != nil {
		log.Debug().Err(cacheErr).Msg("failed to read JWKS cache; fetching fresh keys")
	} else if cached != nil && len(cached.Keys) > 0 && time.Since(cachedAt) <= jwksCacheTTL {
		return cached, cachedAt, nil
	}
	keySet, err := fetchJWKS(authDomain)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err := writeJWKSCache(authDomain, keySet); err != nil {
		log.Debug().Err(err).Msg("failed to write JWKS cache; continuing with fetched keys")
	}
	return keySet, time.Time{}, nil
}

// verifyMetadataWithRetry verifies the metadata JWT against JWKS. A failed
// verification retries once only when the cached keys are old enough to refresh.
func verifyMetadataWithRetry(rawJWT string, authDomain url.URL) (*metadataClaims, error) {
	keySet, cachedAt, err := getJWKSWithCache(authDomain)
	if err != nil {
		return nil, err
	}

	claims, err := verifyMetadataJWT(rawJWT, keySet)
	if err == nil {
		return claims, nil
	}
	if cachedAt.IsZero() || time.Since(cachedAt) < jwksMinRefreshInterval {
		return nil, err
	}

	// Verification failed; this could be key rotation. Re-fetch JWKS and retry.
	keySet, fetchErr := fetchJWKS(authDomain)
	if fetchErr != nil {
		return nil, fmt.Errorf("metadata JWT verification failed (%v) and JWKS refresh also failed: %w", err, fetchErr)
	}
	if err := writeJWKSCache(authDomain, keySet); err != nil {
		log.Debug().Err(err).Msg("failed to write refreshed JWKS cache; continuing with fetched keys")
	}

	return verifyMetadataJWT(rawJWT, keySet)
}
