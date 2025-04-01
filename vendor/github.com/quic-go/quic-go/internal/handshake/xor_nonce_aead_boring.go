//go:build boringcrypto

package handshake

import (
	"crypto/cipher"
	"crypto/tls"
	"os"
	"strings"
)

var goBoringDisabled bool = strings.TrimSpace(os.Getenv("QUIC_GO_DISABLE_BORING")) == "1"

func newAEAD(aes cipher.Block) (cipher.AEAD, error) {
	if goBoringDisabled {
		// In case Go Boring is disabled then
		// fallback to normal cryptographic procedure.
		return cipher.NewGCM(aes)
	}
	return tls.NewGCMTLS13(aes)
}

func allZeros(nonce []byte) bool {
	for _, e := range nonce {
		if e != 0 {
			return false
		}
	}
	return true
}

func (f *xorNonceAEAD) sealZeroNonce() {
	f.doSeal([]byte{}, []byte{}, []byte{}, []byte{})
}

func (f *xorNonceAEAD) seal(nonce, out, plaintext, additionalData []byte) []byte {
	if !goBoringDisabled {
		if !f.hasSeenNonceZero {
			// BoringSSL expects that the first nonce passed to the
			// AEAD instance is zero.
			// At this point the nonce argument is either zero or
			// an artificial one will be passed to the AEAD through
			// [sealZeroNonce]
			f.hasSeenNonceZero = true
			if !allZeros(nonce) {
				f.sealZeroNonce()
			}
		}
	}
	return f.doSeal(nonce, out, plaintext, additionalData)
}
