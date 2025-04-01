//go:build !boringcrypto

package handshake

import "crypto/cipher"

func newAEAD(aes cipher.Block) (cipher.AEAD, error) {
	return cipher.NewGCM(aes)
}

func (f *xorNonceAEAD) seal(nonce, out, plaintext, additionalData []byte) []byte {
	return f.doSeal(nonce, out, plaintext, additionalData)
}
