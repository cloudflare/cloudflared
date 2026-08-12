package token

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/nacl/box"
)

// newEncrypterFromKeys builds an Encrypter around a caller-supplied keypair.
//
// This exists only so tests can exercise Decrypt deterministically. It is intentionally
// unexported and test-only: production code must always go through NewEncrypter, which
// generates an ephemeral keypair from crypto/rand.
func newEncrypterFromKeys(publicKey, privateKey *[32]byte) *Encrypter {
	return &Encrypter{privateKey: privateKey, publicKey: publicKey}
}

// writePEM writes a PEM block of the given type holding the raw key bytes, mirroring the
// format the removed disk-backed key loader used to accept.
func writePEM(t *testing.T, path, pemType string, key *[32]byte) {
	t.Helper()
	data := pem.EncodeToMemory(&pem.Block{Type: pemType, Bytes: key[:]})
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

// TestNewEncrypterIgnoresPlantedCWDKeys is the regression test for the login transfer
// keypair vulnerability: NewEncrypter used to load "cloudflared_priv.pem" and
// "cloudflared_pub.pem" from the process working directory, so an attacker who planted
// those files (via a cloned repo, extracted archive, or shared directory) could predict
// the transfer-service capability URL and steal the victim's origin cert or Access JWTs.
//
// This test does not call t.Parallel: t.Chdir mutates process-wide state.
func TestNewEncrypterIgnoresPlantedCWDKeys(t *testing.T) {
	dir := t.TempDir()

	attackerPub, attackerPriv, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)

	writePEM(t, filepath.Join(dir, "cloudflared_priv.pem"), "BOX PRIVATE KEY", attackerPriv)
	writePEM(t, filepath.Join(dir, "cloudflared_pub.pem"), "PUBLIC KEY", attackerPub)

	t.Chdir(dir)

	encrypter, err := NewEncrypter()
	require.NoError(t, err)

	plantedCapability := base64.URLEncoding.EncodeToString(attackerPub[:])
	assert.NotEqual(t, plantedCapability, encrypter.PublicKey(),
		"NewEncrypter must ignore planted keys in the working directory; the transfer capability must not be attacker-predictable")
	assert.NotEqual(t, attackerPriv, encrypter.privateKey,
		"NewEncrypter must not adopt a private key planted in the working directory")
}

func TestNewEncrypterGeneratesUniqueKeypairs(t *testing.T) {
	t.Parallel()

	first, err := NewEncrypter()
	require.NoError(t, err)
	second, err := NewEncrypter()
	require.NoError(t, err)

	assert.NotEqual(t, first.PublicKey(), second.PublicKey(),
		"each login must get a fresh, unguessable capability")
	assert.NotEqual(t, first.privateKey, second.privateKey)
}

func TestPublicKeyIsBase64URLEncoded32Bytes(t *testing.T) {
	t.Parallel()

	encrypter, err := NewEncrypter()
	require.NoError(t, err)

	decoded, err := base64.URLEncoding.DecodeString(encrypter.PublicKey())
	require.NoError(t, err, "PublicKey must be base64url decodable by the transfer service")
	assert.Len(t, decoded, 32)
	assert.Equal(t, encrypter.publicKey[:], decoded)
}

// TestDecryptRoundTrip mirrors the transfer service flow: the peer seals a payload to our
// public key and prefixes the nonce, and we open it using the peer's public key.
func TestDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	clientPub, clientPriv, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)
	peerPub, peerPriv, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)

	client := newEncrypterFromKeys(clientPub, clientPriv)
	payload := []byte(`{"app_token":"app","org_token":"org"}`)

	var nonce [24]byte
	_, err = rand.Read(nonce[:])
	require.NoError(t, err)
	sealed := box.Seal(nonce[:], payload, &nonce, clientPub, peerPriv)

	decrypted, err := client.Decrypt(sealed, base64.URLEncoding.EncodeToString(peerPub[:]))
	require.NoError(t, err)
	assert.Equal(t, payload, decrypted)
}

func TestDecryptRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	clientPub, clientPriv, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)
	peerPub, _, err := box.GenerateKey(rand.Reader)
	require.NoError(t, err)

	client := newEncrypterFromKeys(clientPub, clientPriv)
	peerCapability := base64.URLEncoding.EncodeToString(peerPub[:])

	tests := []struct {
		name           string
		data           []byte
		senderPubKey   string
		expectedErrMsg string
	}{
		{
			name:           "empty payload",
			data:           nil,
			senderPubKey:   peerCapability,
			expectedErrMsg: "message is too short to contain a nonce",
		},
		{
			name:           "payload shorter than nonce",
			data:           make([]byte, 23),
			senderPubKey:   peerCapability,
			expectedErrMsg: "message is too short to contain a nonce",
		},
		{
			name:           "nonce only, no ciphertext",
			data:           make([]byte, 24),
			senderPubKey:   peerCapability,
			expectedErrMsg: "failed to decrypt message",
		},
		{
			name:           "ciphertext not sealed to us",
			data:           make([]byte, 64),
			senderPubKey:   peerCapability,
			expectedErrMsg: "failed to decrypt message",
		},
		{
			name:           "sender public key is not base64",
			data:           make([]byte, 64),
			senderPubKey:   "!!!not base64!!!",
			expectedErrMsg: "illegal base64 data",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decrypted, err := client.Decrypt(test.data, test.senderPubKey)
			require.Error(t, err)
			assert.Nil(t, decrypted)
			assert.Contains(t, err.Error(), test.expectedErrMsg)
		})
	}
}
