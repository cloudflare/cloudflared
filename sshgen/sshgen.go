package sshgen

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudflare/cloudflared/cmd/cloudflared/config"
	cfpath "github.com/cloudflare/cloudflared/cmd/cloudflared/path"
	"github.com/coreos/go-oidc/jose"
	homedir "github.com/mitchellh/go-homedir"
	gossh "golang.org/x/crypto/ssh"
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

// GenerateShortLivedCertificate generates and stores a keypair for short lived certs
func GenerateShortLivedCertificate(appURL *url.URL, token string) error {
	fullName, err := cfpath.GenerateFilePathFromURL(appURL, keyName)
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
	if token == "" {
		return "", errors.New("invalid token")
	}

	jwt, err := jose.ParseJWT(token)
	if err != nil {
		return "", err
	}

	claims, err := jwt.Claims()
	if err != nil {
		return "", err
	}

	issuer, _, err := claims.StringClaim("iss")
	if err != nil {
		return "", err
	}

	pub, err := generateKeyPair(fullName)
	if err != nil {
		return "", err
	}

	buf, err := json.Marshal(&signPayload{
		PublicKey: string(pub),
		JWT:       token,
		Issuer:    issuer,
	})
	if err != nil {
		return "", err
	}

	res, err := http.Post(issuer+signEndpoint, "application/json", bytes.NewBuffer(buf))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	decoder := json.NewDecoder(res.Body)
	var signRes signResponse
	if err := decoder.Decode(&signRes); err != nil {
		return "", err
	}
	return signRes.Certificate, err
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
		return ioutil.ReadFile(pubKeyName)
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

	return ioutil.WriteFile(filepath, data, 0600)
}
