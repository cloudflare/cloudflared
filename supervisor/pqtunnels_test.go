package supervisor

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/features"
)

func TestCurvePreferences(t *testing.T) {
	// This tests if the correct curves are returned
	// given a PostQuantumMode and a FIPS enabled bool
	t.Parallel()

	tests := []struct {
		name           string
		currentCurves  []tls.CurveID
		expectedCurves []tls.CurveID
		pqMode         features.PostQuantumMode
		fipsEnabled    bool
	}{
		{
			name:           "FIPS with Prefer PQ",
			pqMode:         features.PostQuantumPrefer,
			fipsEnabled:    true,
			currentCurves:  []tls.CurveID{tls.CurveP384},
			expectedCurves: []tls.CurveID{P256Kyber768Draft00PQKex, tls.CurveP256},
		},
		{
			name:           "FIPS with Strict PQ",
			pqMode:         features.PostQuantumStrict,
			fipsEnabled:    true,
			currentCurves:  []tls.CurveID{tls.CurveP256, tls.CurveP384},
			expectedCurves: []tls.CurveID{P256Kyber768Draft00PQKex},
		},
		{
			name:           "FIPS with Prefer PQ - no duplicates",
			pqMode:         features.PostQuantumPrefer,
			fipsEnabled:    true,
			currentCurves:  []tls.CurveID{tls.CurveP256},
			expectedCurves: []tls.CurveID{P256Kyber768Draft00PQKex, tls.CurveP256},
		},
		{
			name:           "Non FIPS with Prefer PQ",
			pqMode:         features.PostQuantumPrefer,
			fipsEnabled:    false,
			currentCurves:  []tls.CurveID{tls.CurveP256},
			expectedCurves: []tls.CurveID{X25519MLKEM768PQKex, X25519Kyber768Draft00PQKex, tls.CurveP256},
		},
		{
			name:           "Non FIPS with Prefer PQ - no duplicates",
			pqMode:         features.PostQuantumPrefer,
			fipsEnabled:    false,
			currentCurves:  []tls.CurveID{X25519Kyber768Draft00PQKex, tls.CurveP256},
			expectedCurves: []tls.CurveID{X25519MLKEM768PQKex, X25519Kyber768Draft00PQKex, tls.CurveP256},
		},
		{
			name:           "Non FIPS with Prefer PQ - correct preference order",
			pqMode:         features.PostQuantumPrefer,
			fipsEnabled:    false,
			currentCurves:  []tls.CurveID{tls.CurveP256, X25519Kyber768Draft00PQKex},
			expectedCurves: []tls.CurveID{X25519MLKEM768PQKex, X25519Kyber768Draft00PQKex, tls.CurveP256},
		},
		{
			name:           "Non FIPS with Strict PQ",
			pqMode:         features.PostQuantumStrict,
			fipsEnabled:    false,
			currentCurves:  []tls.CurveID{tls.CurveP256},
			expectedCurves: []tls.CurveID{X25519MLKEM768PQKex, X25519Kyber768Draft00PQKex},
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()
			curves, err := curvePreference(tcase.pqMode, tcase.fipsEnabled, tcase.currentCurves)
			require.NoError(t, err)
			assert.Equal(t, tcase.expectedCurves, curves)
		})
	}
}
