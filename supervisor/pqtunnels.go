package supervisor

import (
	"crypto/tls"
	"fmt"

	"github.com/cloudflare/cloudflared/features"
)

const (
	X25519Kyber768Draft00PQKex     = tls.CurveID(0x6399) // X25519Kyber768Draft00
	X25519Kyber768Draft00PQKexName = "X25519Kyber768Draft00"
	P256Kyber768Draft00PQKex       = tls.CurveID(0xfe32) // P256Kyber768Draft00
	P256Kyber768Draft00PQKexName   = "P256Kyber768Draft00"
	X25519MLKEM768PQKex            = tls.CurveID(0x11ec) // X25519MLKEM768
	X25519MLKEM768PQKexName        = "X25519MLKEM768"
)

var (
	nonFipsPostQuantumStrictPKex []tls.CurveID = []tls.CurveID{X25519MLKEM768PQKex}
	nonFipsPostQuantumPreferPKex []tls.CurveID = []tls.CurveID{X25519MLKEM768PQKex}
	fipsPostQuantumStrictPKex    []tls.CurveID = []tls.CurveID{P256Kyber768Draft00PQKex}
	fipsPostQuantumPreferPKex    []tls.CurveID = []tls.CurveID{P256Kyber768Draft00PQKex, tls.CurveP256}
)

func removeDuplicates(curves []tls.CurveID) []tls.CurveID {
	bucket := make(map[tls.CurveID]bool)
	var result []tls.CurveID
	for _, curve := range curves {
		if _, ok := bucket[curve]; !ok {
			bucket[curve] = true
			result = append(result, curve)
		}
	}
	return result
}

func curvePreference(pqMode features.PostQuantumMode, fipsEnabled bool, currentCurve []tls.CurveID) ([]tls.CurveID, error) {
	switch pqMode {
	case features.PostQuantumStrict:
		// If the user passes the -post-quantum flag, we override
		// CurvePreferences to only support hybrid post-quantum key agreements.
		if fipsEnabled {
			return fipsPostQuantumStrictPKex, nil
		}
		return nonFipsPostQuantumStrictPKex, nil
	case features.PostQuantumPrefer:
		if fipsEnabled {
			// Ensure that all curves returned are FIPS compliant.
			// Moreover the first curves are post-quantum and then the
			// non post-quantum.
			return fipsPostQuantumPreferPKex, nil
		}
		curves := append(nonFipsPostQuantumPreferPKex, currentCurve...)
		curves = removeDuplicates(curves)
		return curves, nil
	default:
		return nil, fmt.Errorf("Unexpected post quantum mode")
	}
}
