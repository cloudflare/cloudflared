package handshake

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"sync"

	"github.com/quic-go/quic-go/internal/protocol"
)

var (
	retryAEADdraft29 cipher.AEAD // used for QUIC draft versions up to 34
	retryAEADv1      cipher.AEAD // used for QUIC v1 (RFC 9000)
	retryAEADv2      cipher.AEAD // used for QUIC v2
)

func init() {
	retryAEADdraft29 = initAEAD([16]byte{0xcc, 0xce, 0x18, 0x7e, 0xd0, 0x9a, 0x09, 0xd0, 0x57, 0x28, 0x15, 0x5a, 0x6c, 0xb9, 0x6b, 0xe1})
	retryAEADv1 = initAEAD([16]byte{0xbe, 0x0c, 0x69, 0x0b, 0x9f, 0x66, 0x57, 0x5a, 0x1d, 0x76, 0x6b, 0x54, 0xe3, 0x68, 0xc8, 0x4e})
	retryAEADv2 = initAEAD([16]byte{0x8f, 0xb4, 0xb0, 0x1b, 0x56, 0xac, 0x48, 0xe2, 0x60, 0xfb, 0xcb, 0xce, 0xad, 0x7c, 0xcc, 0x92})
}

func initAEAD(key [16]byte) cipher.AEAD {
	aes, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(aes)
	if err != nil {
		panic(err)
	}
	return aead
}

var (
	retryBuf          bytes.Buffer
	retryMutex        sync.Mutex
	retryNonceDraft29 = [12]byte{0xe5, 0x49, 0x30, 0xf9, 0x7f, 0x21, 0x36, 0xf0, 0x53, 0x0a, 0x8c, 0x1c}
	retryNonceV1      = [12]byte{0x46, 0x15, 0x99, 0xd3, 0x5d, 0x63, 0x2b, 0xf2, 0x23, 0x98, 0x25, 0xbb}
	retryNonceV2      = [12]byte{0xd8, 0x69, 0x69, 0xbc, 0x2d, 0x7c, 0x6d, 0x99, 0x90, 0xef, 0xb0, 0x4a}
)

// GetRetryIntegrityTag calculates the integrity tag on a Retry packet
func GetRetryIntegrityTag(retry []byte, origDestConnID protocol.ConnectionID, version protocol.VersionNumber) *[16]byte {
	retryMutex.Lock()
	defer retryMutex.Unlock()

	retryBuf.WriteByte(uint8(origDestConnID.Len()))
	retryBuf.Write(origDestConnID.Bytes())
	retryBuf.Write(retry)
	defer retryBuf.Reset()

	var tag [16]byte
	var sealed []byte
	//nolint:exhaustive // These are all the versions we support
	switch version {
	case protocol.Version1:
		sealed = retryAEADv1.Seal(tag[:0], retryNonceV1[:], nil, retryBuf.Bytes())
	case protocol.Version2:
		sealed = retryAEADv2.Seal(tag[:0], retryNonceV2[:], nil, retryBuf.Bytes())
	default:
		sealed = retryAEADdraft29.Seal(tag[:0], retryNonceDraft29[:], nil, retryBuf.Bytes())
	}
	if len(sealed) != 16 {
		panic(fmt.Sprintf("unexpected Retry integrity tag length: %d", len(sealed)))
	}
	return &tag
}
