package v3_test

import (
	"testing"
)

// FuzzSessionWrite verifies that we don't run into any panics when writing a single variable sized payload to the origin.
func FuzzSessionWrite(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		// The origin transport read is bound to 1280 bytes
		if len(b) > 1280 {
			b = b[:1280]
		}
		testSessionWrite(t, [][]byte{b})
	})
}

// FuzzSessionRead verifies that we don't run into any panics when reading a single variable sized payload from the origin.
func FuzzSessionRead(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		// The origin transport read is bound to 1280 bytes
		if len(b) > 1280 {
			b = b[:1280]
		}
		testSessionRead(t, [][]byte{b})
	})
}
