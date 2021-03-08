// Package encrypter is suitable for encrypting messages you would like to securely share between two points.
// Useful for providing end to end encryption (E2EE). It uses Box (NaCl) for encrypting the messages.
// tldr is it uses Elliptic Curves (Curve25519) for the keys, XSalsa20 and Poly1305 for encryption.
// You can read more here https://godoc.org/golang.org/x/crypto/nacl/box.
//
//		msg := []byte("super safe message.")
//		alice, err := NewEncrypter("alice_priv_key.pem", "alice_pub_key.pem")
//		if err != nil {
//			log.Fatal(err)
//		}
//
//		bob, err := NewEncrypter("bob_priv_key.pem", "bob_pub_key.pem")
//		if err != nil {
//			log.Fatal(err)
//		}
//		encrypted, err := alice.Encrypt(msg, bob.PublicKey())
//		if err != nil {
//			log.Fatal(err)
//		}
//
//		data, err := bob.Decrypt(encrypted, alice.PublicKey())
//		if err != nil {
//			log.Fatal(err)
//		}
//		fmt.Println(string(data))
package token

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"os"

	"golang.org/x/crypto/nacl/box"
)

// Encrypter represents a keypair value with auxiliary functions to make
// doing encryption and decryption easier
type Encrypter struct {
	privateKey *[32]byte
	publicKey  *[32]byte
}

// NewEncrypter returns a new encrypter with initialized keypair
func NewEncrypter(privateKey, publicKey string) (*Encrypter, error) {
	e := &Encrypter{}
	pubKey, key, err := e.fetchOrGenerateKeys(privateKey, publicKey)
	if err != nil {
		return nil, err
	}
	e.privateKey, e.publicKey = key, pubKey
	return e, nil
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

// Encrypt data using our privateKey and the recipient publicKey
// data is a buffer of data that we would like to encrypt. Messages will have the nonce added to front
// as they have to unique for each message shared.
// recipientPublicKey is a base64 encoded version of the sender's public key (most likely from the PublicKey function).
// The return value is the encrypted buffer or an error.
func (e *Encrypter) Encrypt(data []byte, recipientPublicKey string) ([]byte, error) {
	var nonce [24]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	pubKey, err := e.decodePublicKey(recipientPublicKey)
	if err != nil {
		return nil, err
	}
	// This encrypts msg and adds the nonce to the front of the message, since the nonce has to be
	// the same for encrypting and decrypting
	return box.Seal(nonce[:], data, &nonce, pubKey, e.privateKey), nil
}

// WriteKeys keys will take the currently initialized keypair and write them to provided filenames
func (e *Encrypter) WriteKeys(privateKey, publicKey string) error {
	if err := e.writeKey(e.privateKey[:], "BOX PRIVATE KEY", privateKey); err != nil {
		return err
	}
	return e.writeKey(e.publicKey[:], "PUBLIC KEY", publicKey)
}

// fetchOrGenerateKeys will either load or create a keypair if it doesn't exist
func (e *Encrypter) fetchOrGenerateKeys(privateKey, publicKey string) (*[32]byte, *[32]byte, error) {
	key, err := e.fetchKey(privateKey)
	if os.IsNotExist(err) {
		return box.GenerateKey(rand.Reader)
	} else if err != nil {
		return nil, nil, err
	}

	pub, err := e.fetchKey(publicKey)
	if os.IsNotExist(err) {
		return box.GenerateKey(rand.Reader)
	} else if err != nil {
		return nil, nil, err
	}
	return pub, key, nil
}

// writeKey will write a key to disk in DER format (it's a standard pem key)
func (e *Encrypter) writeKey(key []byte, pemType, filename string) error {
	data := pem.EncodeToMemory(&pem.Block{
		Type:  pemType,
		Bytes: key,
	})

	f, err := os.Create(filename)
	if err != nil {
		return err
	}

	_, err = f.Write(data)
	if err != nil {
		return err
	}

	return nil
}

// fetchKey will load a a DER formatted key from disk
func (e *Encrypter) fetchKey(filename string) (*[32]byte, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	buf := new(bytes.Buffer)
	io.Copy(buf, f)

	p, _ := pem.Decode(buf.Bytes())
	if p == nil {
		return nil, errors.New("Failed to decode key")
	}
	var newKey [32]byte
	copy(newKey[:], p.Bytes)

	return &newKey, nil
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
