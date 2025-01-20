package sshgen

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
	gossh "golang.org/x/crypto/ssh"

	"github.com/cloudflare/cloudflared/config"
	cfpath "github.com/cloudflare/cloudflared/token"
)

const (
	signEndpoint = "/cdn-cgi/access/cert_sign"
	keyName      = "cf_key"
)

// signPayload represents the request body sent to the sign handler API
type signPayload struct {
	PublicKey string `json:"public_key"`
	JWT       string `json:"jwt"`
	Issuer    string `json:"issuer"`
}

// signResponse represents the response body from the sign handler API
type signResponse struct {
	KeyID       string    `json:"id"`
	Certificate string    `json:"certificate"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ErrorResponse struct stores error information after any error-prone function
type errorResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

var mockRequest func(url, contentType string, body io.Reader) (*http.Response, error) = nil

var signatureAlgs = []jose.SignatureAlgorithm{jose.RS256}

// GenerateShortLivedCertificate generates and stores a keypair for short lived certs
func GenerateShortLivedCertificate(appURL *url.URL, token string) error {
	fullName, err := cfpath.GenerateSSHCertFilePathFromURL(appURL, keyName)
	if err != nil {
		return err
	}

	cert, err := handleCertificateGeneration(token, fullName)
	if err != nil {
		return err
	}

	name := fullName + "-cert.pub"
	if err := writeKey(name, []byte(cert)); err != nil {
		return err
	}

	return nil
}

// handleCertificateGeneration takes a JWT and uses it build a signPayload
// to send to the Sign endpoint with the public key from the keypair it generated
func handleCertificateGeneration(token, fullName string) (string, error) {
	pub, err := generateKeyPair(fullName)
	if err != nil {
		return "", err
	}

	return SignCert(token, string(pub))
}

func SignCert(token, pubKey string) (string, error) {
	if token == "" {
		return "", errors.New("invalid token")
	}

	parsedToken, err := jwt.ParseSigned(token, signatureAlgs)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse JWT")
	}

	claims := jwt.Claims{}
	err = parsedToken.UnsafeClaimsWithoutVerification(&claims)
	if err != nil {
		return "", errors.Wrap(err, "failed to retrieve JWT claims")
	}

	buf, err := json.Marshal(&signPayload{
		PublicKey: pubKey,
		JWT:       token,
		Issuer:    claims.Issuer,
	})
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal signPayload")
	}
	var res *http.Response
	if mockRequest != nil {
		res, err = mockRequest(claims.Issuer+signEndpoint, "application/json", bytes.NewBuffer(buf))
	} else {
		client := http.Client{
			Timeout: 10 * time.Second,
		}
		res, err = client.Post(claims.Issuer+signEndpoint, "application/json", bytes.NewBuffer(buf))
	}

	if err != nil {
		return "", errors.Wrap(err, "failed to send request")
	}
	defer res.Body.Close()

	decoder := json.NewDecoder(res.Body)

	if res.StatusCode != 200 {
		var errResponse errorResponse
		if err := decoder.Decode(&errResponse); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%d: %s", errResponse.Status, errResponse.Message)
	}

	var signRes signResponse
	if err := decoder.Decode(&signRes); err != nil {
		return "", errors.Wrap(err, "failed to decode HTTP response")
	}
	return signRes.Certificate, nil
}

// generateKeyPair creates a EC keypair (P256) and stores them in the homedir.
// returns the generated public key from the successful keypair generation
func generateKeyPair(fullName string) ([]byte, error) {
	pubKeyName := fullName + ".pub"

	exist, err := config.FileExists(pubKeyName)
	if err != nil {
		return nil, err
	}
	if exist {
		return os.ReadFile(pubKeyName)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	parsed, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}

	if err := writeKey(fullName, pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: parsed,
	})); err != nil {
		return nil, err
	}

	pub, err := gossh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	data := gossh.MarshalAuthorizedKey(pub)

	if err := writeKey(pubKeyName, data); err != nil {
		return nil, err
	}

	return data, nil
}

// writeKey will write a key to disk in DER format (it's a standard pem key)
func writeKey(filename string, data []byte) error {
	filepath, err := homedir.Expand(filename)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0600)
}
