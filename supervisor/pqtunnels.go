package supervisor

import (
	"crypto/tls"
	"fmt"

	"github.com/cloudflare/cloudflared/features"
)

// When experimental post-quantum tunnels are enabled, and we're hitting an
// issue creating the tunnel, we'll report the first error
// to https://pqtunnels.cloudflareresearch.com.

const (
	PQKex     = tls.CurveID(0x6399) // X25519Kyber768Draft00
	PQKexName = "X25519Kyber768Draft00"
)

func curvePreference(pqMode features.PostQuantumMode, currentCurve []tls.CurveID) ([]tls.CurveID, error) {
	switch pqMode {
	case features.PostQuantumStrict:
		// If the user passes the -post-quantum flag, we override
		// CurvePreferences to only support hybrid post-quantum key agreements.
		return []tls.CurveID{PQKex}, nil
	case features.PostQuantumPrefer:
		if len(currentCurve) == 0 {
			return []tls.CurveID{PQKex}, nil
		}

		if currentCurve[0] != PQKex {
			return append([]tls.CurveID{PQKex}, currentCurve...), nil
		}
		return currentCurve, nil
	default:
		return nil, fmt.Errorf("Unexpected post quantum mode")
	}
}
