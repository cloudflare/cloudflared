package protocol

import (
	"crypto/rand"
	"encoding/hex"
)

// GenerateEventID generates a random UUID v4 for use as a Sentry event ID.
func GenerateEventID() string {
	id := make([]byte, 16)
	// Prefer rand.Read over rand.Reader, see https://go-review.googlesource.com/c/go/+/272326/.
	_, _ = rand.Read(id)
	id[6] &= 0x0F // clear version
	id[6] |= 0x40 // set version to 4 (random uuid)
	id[8] &= 0x3F // clear variant
	id[8] |= 0x80 // set to IETF variant
	return hex.EncodeToString(id)
}
