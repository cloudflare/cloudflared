package crypto

import (
	"crypto/tls"
	"errors"
	"fmt"
	"slices"

	"github.com/cloudflare/cloudflared/features"
)

// errUnknownPostQuantumMode is returned by GetCurvePreferences when the
// caller passes a features.PostQuantumMode value that is not one of the
// documented constants. It is intentionally unexported: callers should treat
// any non-nil error as a programming mistake rather than inspecting it.
var errUnknownPostQuantumMode = errors.New("the provided post quantum mode is unknown")

// P256Kyber768Draft00 is a post-quantum KEM based on Kyber768.
const P256Kyber768Draft00 = tls.CurveID(0xfe32) // ID 65074

// Canonical curve lists returned by GetCurvePreferences. They are kept
// package-private so that callers cannot accidentally mutate the shared
// slice; GetCurvePreferences always returns a clone.
var (
	// postQuantumStrictCurves is used when the caller requires a
	// post-quantum handshake. Only PQ curves (X25519MLKEM768 and the
	// deprecated P256Kyber768Draft00 for backward compatibility) are
	// advertised; no classical-only curve is included.
	postQuantumStrictCurves = []tls.CurveID{tls.X25519MLKEM768, P256Kyber768Draft00}
	// postQuantumPreferCurves is used for the default "prefer" mode: the PQ
	// curve is advertised first and the classical CurveP256 is listed as a
	// fallback so peers without PQ support can still negotiate.
	postQuantumPreferCurves = []tls.CurveID{tls.X25519MLKEM768, P256Kyber768Draft00, tls.CurveP256}
)

// getCurvePreferences returns the TLS curve preferences that should be
// applied to edge-facing connections for the given post-quantum mode.
//
// The returned slice is the canonical, protocol-agnostic curve list and is
// suitable for direct assignment to tls.Config.CurvePreferences. A fresh
// slice is returned on every call, so callers may mutate it freely without
// affecting other callers.
//
// An error is returned only when profile is not a recognised
// features.PostQuantumMode value, which indicates a programming bug in the
// caller.
func getCurvePreferences(profile features.PostQuantumMode) ([]tls.CurveID, error) {
	switch profile {
	case features.PostQuantumPrefer:
		return slices.Clone(postQuantumPreferCurves), nil
	case features.PostQuantumStrict:
		return slices.Clone(postQuantumStrictCurves), nil
	}

	return nil, errUnknownPostQuantumMode
}

// TLSConfigWithCurvePreferences clones the provided tls.Config and applies
// curve preferences based on the given post-quantum mode.
//
// The original tls.Config is never modified; a clone is returned so that
// callers can safely use the same base configuration across multiple
// goroutines without racing on CurvePreferences.
//
// Returns an error only when pqMode is not a recognised
// features.PostQuantumMode value.
func TLSConfigWithCurvePreferences(tlsConfig *tls.Config, pqMode features.PostQuantumMode) (*tls.Config, error) {
	// Clone the TLS config before applying per-connection curve
	// preferences. The TlsConfig may be shared across goroutines;
	// mutating it directly would race with concurrent connection attempts.
	config := tlsConfig.Clone()
	curvePref, err := getCurvePreferences(pqMode)
	if err != nil {
		return nil, fmt.Errorf("get curve preferences: %w", err)
	}

	config.CurvePreferences = curvePref
	return config, nil
}
