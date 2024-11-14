package v3_test

import (
	"testing"
)

// FuzzSessionWrite verifies that we don't run into any panics when writing variable sized payloads to the origin.
func FuzzSessionWrite(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		testSessionWrite(t, b)
	})
}

// FuzzSessionServe verifies that we don't run into any panics when reading variable sized payloads from the origin.
func FuzzSessionServe(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		// The origin transport read is bound to 1280 bytes
		if len(b) > 1280 {
			b = b[:1280]
		}
		testSessionServe_Origin(t, b)
	})
}
