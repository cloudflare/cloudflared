package supervisor

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/features"
	"github.com/cloudflare/cloudflared/fips"
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
			expectedCurves: []tls.CurveID{X25519MLKEM768PQKex, tls.CurveP256},
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
			expectedCurves: []tls.CurveID{X25519MLKEM768PQKex, tls.CurveP256, X25519Kyber768Draft00PQKex},
		},
		{
			name:           "Non FIPS with Strict PQ",
			pqMode:         features.PostQuantumStrict,
			fipsEnabled:    false,
			currentCurves:  []tls.CurveID{tls.CurveP256},
			expectedCurves: []tls.CurveID{X25519MLKEM768PQKex},
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

func runClientServerHandshake(t *testing.T, curves []tls.CurveID) []tls.CurveID {
	var advertisedCurves []tls.CurveID
	ts := httptest.NewUnstartedServer(nil)
	ts.TLS = &tls.Config{ // nolint: gosec
		GetConfigForClient: func(chi *tls.ClientHelloInfo) (*tls.Config, error) {
			advertisedCurves = slices.Clone(chi.SupportedCurves)
			return nil, nil
		},
	}
	ts.StartTLS()
	defer ts.Close()
	clientTlsConfig := ts.Client().Transport.(*http.Transport).TLSClientConfig
	clientTlsConfig.CurvePreferences = curves
	resp, err := ts.Client().Head(ts.URL)
	if err != nil {
		t.Error(err)
		return nil
	}
	defer resp.Body.Close()
	return advertisedCurves
}

func TestSupportedCurvesNegotiation(t *testing.T) {
	for _, tcase := range []features.PostQuantumMode{features.PostQuantumPrefer} {
		curves, err := curvePreference(tcase, fips.IsFipsEnabled(), make([]tls.CurveID, 0))
		require.NoError(t, err)
		advertisedCurves := runClientServerHandshake(t, curves)
		assert.Equal(t, curves, advertisedCurves)
	}
}
