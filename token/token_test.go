package token

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

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

// craftTestToken builds a compact JWS string from the given raw JSON header,
// payload, and fake signature bytes. This lets tests control exact header key
// ordering.
func craftTestToken(header, payload, sig string) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(header))
	p := base64.RawURLEncoding.EncodeToString([]byte(payload))
	s := base64.RawURLEncoding.EncodeToString([]byte(sig))
	return h + "." + p + "." + s
}

func TestGetTokenIfExists_PreservesOriginalTokenString(t *testing.T) {
	t.Parallel()

	// Header with non-alphabetical key ordering: "typ" before "alg".
	// go-jose's CompactSerialize() re-marshals header JSON with Go's default
	// alphabetical key order ("alg" before "typ"), producing different base64url
	// bytes and invalidating the JWT signature.
	header := `{"typ":"JWT","alg":"RS256"}`
	payload := `{"aud":"test-aud","exp":9999999999,"iat":1,"nbf":1,"sub":"test","email":"test@test.com","iss":"test"}`
	originalToken := craftTestToken(header, payload, "fake-sig")

	tmpFile := filepath.Join(t.TempDir(), "test-token")
	require.NoError(t, os.WriteFile(tmpFile, []byte(originalToken), 0600))

	raw, token, err := getTokenIfExists(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, token)

	// The returned raw string must be byte-for-byte identical to what was on disk.
	assert.Equal(t, originalToken, raw)

	// CompactSerialize() re-encodes the header with alphabetical keys, which
	// produces a different string — exactly the bug this change fixes.
	reserialized, err := token.CompactSerialize()
	require.NoError(t, err)
	assert.NotEqual(t, originalToken, reserialized,
		"CompactSerialize() reorders header keys, which would invalidate the JWT signature")
}

func TestGetTokenIfExists_TrimsWhitespace(t *testing.T) {
	t.Parallel()

	header := `{"alg":"RS256"}`
	payload := `{"aud":"test-aud","exp":9999999999,"iat":1,"nbf":1,"sub":"test","email":"test@test.com","iss":"test"}`
	token := craftTestToken(header, payload, "fake-sig")

	// Simulate a file with trailing newline/whitespace (common with text editors).
	tokenWithWhitespace := "  " + token + "  \n"
	tmpFile := filepath.Join(t.TempDir(), "test-token-ws")
	require.NoError(t, os.WriteFile(tmpFile, []byte(tokenWithWhitespace), 0600))

	raw, parsed, err := getTokenIfExists(tmpFile)
	require.NoError(t, err)
	require.NotNil(t, parsed)

	assert.Equal(t, token, raw, "raw token should have surrounding whitespace trimmed")
}

func TestGetTokenIfExists_FileNotFound(t *testing.T) {
	t.Parallel()

	raw, token, err := getTokenIfExists(filepath.Join(t.TempDir(), "nonexistent"))
	assert.Error(t, err)
	assert.Empty(t, raw)
	assert.Nil(t, token)
}

func TestGetTokenIfExists_InvalidToken(t *testing.T) {
	t.Parallel()

	tmpFile := filepath.Join(t.TempDir(), "bad-token")
	require.NoError(t, os.WriteFile(tmpFile, []byte("not-a-valid-jws"), 0600))

	raw, token, err := getTokenIfExists(tmpFile)
	assert.Error(t, err)
	assert.Empty(t, raw)
	assert.Nil(t, token)
}
