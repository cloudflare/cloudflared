//go:build !windows
// +build !windows

package sshgen

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/stretchr/testify/assert"

	"github.com/cloudflare/cloudflared/config"
	cfpath "github.com/cloudflare/cloudflared/token"
)

const (
	audTest   = "cf-test-aud"
	nonceTest = "asfd"
)

type signingArguments struct {
	Principals   []string `json:"principals"`
	ClientPubKey string   `json:"public_key"`
	Duration     string   `json:"duration"`
}

func TestCertGenSuccess(t *testing.T) {
	url, _ := url.Parse("https://cf-test-access.com/testpath")
	token := tokenGenerator()

	fullName, err := cfpath.GenerateSSHCertFilePathFromURL(url, keyName)
	assert.NoError(t, err)
	assert.True(t, strings.HasSuffix(fullName, "/cf-test-access.com-testpath-cf_key"))

	pubKeyName := fullName + ".pub"
	certKeyName := fullName + "-cert.pub"

	defer func() {
		os.Remove(fullName)
		os.Remove(pubKeyName)
		os.Remove(certKeyName)
	}()

	resp := signingArguments{
		Principals:   []string{"dalton"},
		ClientPubKey: "ecdsa-sha2-nistp256-cert-v01@openssh.com AAAAKGVjZHNhLXNoYTItbmlzdHAyNTYtY2VydC12MDFAb3BlbnNzaC5jb20AAAAg+0rYq4mNGIAHiH1xPOJXfmOpTEwFIcyXzGJieTOhRs8AAAAIbmlzdHAyNTYAAABBBJIcsq02e8ZaofJXOZKp7yQdKW/JIouJ90lybr76hHIRrZBL1t4JEimfLvNDphPrTW9VDQaIcBSKNaxRqHOS8jezoJbhFGWhqQAAAAEAAAAgZWU5OTliNGRkZmFmNjgxNDEwMTVhMDJiY2ZhMTdiN2UAAAAKAAAABmF1c3RpbgAAAABc1KFoAAAAAFzUohwAAAAAAAAARwAAABdwZXJtaXQtYWdlbnQtZm9yd2FyZGluZwAAAAAAAAAKcGVybWl0LXB0eQAAAAAAAAAOcGVybWl0LXVzZXItcmMAAAAAAAAAAAAAAGgAAAATZWNkc2Etc2hhMi1uaXN0cDI1NgAAAAhuaXN0cDI1NgAAAEEEeAuYR56XaxvH5Z1p0hDCTQ7wC4dbj0Gc+LOKu1f94og2ilZTv9tutg8cZrqAT97REmGH6j9KIOVLGsPVjajSKAAAAGQAAAATZWNkc2Etc2hhMi1uaXN0cDI1NgAAAEkAAAAhAORY9ZO3TQsrUm6ajnVW+arbnVfTkxYBYFlVoeOEXKZuAAAAIG96A8nQnTuprWXLSemWL68RXC1NVKnBOIPD2Z7UIOB1",
		Duration:     "3m",
	}
	w := httptest.NewRecorder()
	respJson, err := json.Marshal(resp)
	assert.NoError(t, err)
	w.Write(respJson)
	mockRequest = func(url, contentType string, body io.Reader) (*http.Response, error) {
		assert.Contains(t, "/cdn-cgi/access/cert_sign", url)
		assert.Equal(t, "application/json", contentType)
		buf, err := io.ReadAll(body)
		assert.NoError(t, err)
		assert.NotEmpty(t, buf)
		return w.Result(), nil
	}

	err = GenerateShortLivedCertificate(url, token)
	assert.NoError(t, err)

	exist, err := config.FileExists(fullName)
	assert.NoError(t, err)
	if !exist {
		assert.FailNow(t, fmt.Sprintf("key should exist at: %s", fullName), fullName)
		return
	}

	exist, err = config.FileExists(pubKeyName)
	assert.NoError(t, err)
	if !exist {
		assert.FailNow(t, fmt.Sprintf("key should exist at: %s", pubKeyName), pubKeyName)
		return
	}

	exist, err = config.FileExists(certKeyName)
	assert.NoError(t, err)
	if !exist {
		assert.FailNow(t, fmt.Sprintf("key should exist at: %s", certKeyName), certKeyName)
		return
	}
}

func tokenGenerator() string {
	iat := time.Now()
	exp := time.Now().Add(time.Minute * 5)

	claims := jwt.Claims{
		Audience: jwt.Audience{audTest},
		IssuedAt: jwt.NewNumericDate(iat),
		Expiry:   jwt.NewNumericDate(exp),
	}

	key := []byte("secret")
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		panic(err)
	}

	signedToken, err := jwt.Signed(signer).Claims(claims).CompactSerialize()
	if err != nil {
		panic(err)
	}

	return signedToken
}
