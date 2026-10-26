// Package token contains the Encrypter, a small helper for end-to-end encrypting (E2EE)
// messages exchanged between two points. It uses Box (NaCl) for encrypting the messages.
// tldr is it uses Elliptic Curves (Curve25519) for the keys, XSalsa20 and Poly1305 for encryption.
// You can read more here https://godoc.org/golang.org/x/crypto/nacl/box.
//
// The keypair is always ephemeral: it is generated from crypto/rand on every call to
// NewEncrypter and is never read from or written to disk. In the login flow the base64url
// public key doubles as the sole capability identifier for retrieving credentials from the
// transfer service, so a predictable or reusable keypair would let anyone who knows it steal
// the credential. Do not add a disk-backed or caller-supplied key path.
//
//	alice, err := NewEncrypter()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	bob, err := NewEncrypter()
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	encrypted := box.Seal(...) // sealed by the remote peer to alice.PublicKey()
//
//	data, err := alice.Decrypt(encrypted, bob.PublicKey())
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(string(data))
package token

import (
	"crypto/rand"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/nacl/box"
)

// Encrypter represents a keypair value with auxiliary functions to make
// doing encryption and decryption easier
type Encrypter struct {
	privateKey *[32]byte
	publicKey  *[32]byte
}

// NewEncrypter returns a new encrypter with a freshly generated ephemeral keypair.
//
// The keypair is always generated from crypto/rand. It is deliberately not loadable from
// disk or supplied by the caller: the public key is used as an unguessable capability in
// the login transfer flow, so any reusable or attacker-plantable key would allow credential
// theft. See newEncrypterFromKeys for the test-only escape hatch.
func NewEncrypter() (*Encrypter, error) {
	pubKey, key, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Encrypter{privateKey: key, publicKey: pubKey}, nil
}

// PublicKey returns a base64 encoded public key. Useful for transport (like in HTTP requests)
func (e *Encrypter) PublicKey() string {
	return base64.URLEncoding.EncodeToString(e.publicKey[:])
}

// Decrypt data that was encrypted using our publicKey. It will use our privateKey and the sender's publicKey to decrypt
// data is an encrypted buffer of data, mostly like from the Encrypt function. Messages contain the nonce data on the front
// of the message.
// senderPublicKey is a base64 encoded version of the sender's public key (most likely from the PublicKey function).
// The return value is the decrypted buffer or an error.
func (e *Encrypter) Decrypt(data []byte, senderPublicKey string) ([]byte, error) {
	if len(data) < 24 {
		return nil, errors.New("message is too short to contain a nonce")
	}
	var decryptNonce [24]byte
	copy(decryptNonce[:], data[:24]) // we pull the nonce from the front of the actual message.
	pubKey, err := e.decodePublicKey(senderPublicKey)
	if err != nil {
		return nil, err
	}
	decrypted, ok := box.Open(nil, data[24:], &decryptNonce, pubKey, e.privateKey)
	if !ok {
		return nil, errors.New("failed to decrypt message")
	}
	return decrypted, nil
}

// decodePublicKey will base64 decode the provided key to the box representation
func (e *Encrypter) decodePublicKey(key string) (*[32]byte, error) {
	pub, err := base64.URLEncoding.DecodeString(key)
	if err != nil {
		return nil, err
	}
	var newKey [32]byte
	copy(newKey[:], pub)
	return &newKey, nil
}
