package crypto

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/features"
)

// TestCurvePreferences verifies that GetCurvePreferences returns the
// documented curve list for each supported PostQuantumMode. The expected
// values correspond to the contract described in the package documentation
// and must be identical under FIPS and non-FIPS builds (see TUN-10413).
func TestCurvePreferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		expectedCurves []tls.CurveID
		pqMode         features.PostQuantumMode
	}{
		{
			name:           "Prefer PQ",
			pqMode:         features.PostQuantumPrefer,
			expectedCurves: []tls.CurveID{tls.X25519MLKEM768, P256Kyber768Draft00, tls.CurveP256},
		},
		{
			name:           "Strict PQ",
			pqMode:         features.PostQuantumStrict,
			expectedCurves: []tls.CurveID{tls.X25519MLKEM768, P256Kyber768Draft00},
		},
	}

	for _, tcase := range tests {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()
			curves, err := getCurvePreferences(tcase.pqMode)
			require.NoError(t, err)
			require.Equal(t, tcase.expectedCurves, curves)
		})
	}
}

// TestCurvePreferenceUnknownMode asserts that passing a PostQuantumMode
// value outside of the documented constants produces an error instead of
// silently returning a nil or default curve list. This protects callers
// from accidentally negotiating with an unintended curve set.
func TestCurvePreferenceUnknownMode(t *testing.T) {
	t.Parallel()

	_, err := getCurvePreferences(features.PostQuantumMode(255))
	require.Error(t, err)
}

// TestReturnedSliceIsIndependent ensures GetCurvePreferences returns a
// fresh slice on every call, so that callers cannot corrupt the
// package-level defaults by mutating the result.
func TestReturnedSliceIsIndependent(t *testing.T) {
	t.Parallel()

	first, err := getCurvePreferences(features.PostQuantumPrefer)
	require.NoError(t, err)
	// Mutate the returned slice.
	first[0] = tls.CurveP521

	second, err := getCurvePreferences(features.PostQuantumPrefer)
	require.NoError(t, err)
	require.Equal(t, tls.X25519MLKEM768, second[0], "package defaults must not be affected by caller mutation")
}

// runClientServerHandshake drives a TLS 1.3 handshake with the given curve
// preferences set on the client and captures the SupportedCurves list
// advertised by the client in its ClientHello. The helper is used by
// TestSupportedCurvesNegotiation to exercise the curves end-to-end against
// the standard library's TLS stack.
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
	clientTLSConfig := ts.Client().Transport.(*http.Transport).TLSClientConfig
	clientTLSConfig.CurvePreferences = curves
	resp, err := ts.Client().Head(ts.URL)
	if err != nil {
		t.Error(err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	return advertisedCurves
}

// TestSupportedCurvesNegotiation verifies that the curves returned by
// GetCurvePreferences survive a real TLS handshake unchanged, i.e. the
// standard library advertises exactly the curves we expect. Currently only
// PostQuantumPrefer is exercised because PostQuantumStrict would cause the
// handshake to fail against httptest servers that do not support
// X25519MLKEM768 server-side.
func TestSupportedCurvesNegotiation(t *testing.T) {
	t.Parallel()
	for _, tcase := range []features.PostQuantumMode{features.PostQuantumPrefer} {
		curves, err := getCurvePreferences(tcase)
		require.NoError(t, err)
		advertisedCurves := runClientServerHandshake(t, curves)
		require.True(t, slices.Contains(advertisedCurves, tls.CurveP256))
		require.True(t, slices.Contains(advertisedCurves, tls.X25519MLKEM768))
		expectedLength := 2
		if runtime.GOOS == "linux" {
			// P256Kyber768Draft00 only exists in linux
			require.True(t, slices.Contains(advertisedCurves, P256Kyber768Draft00))
			expectedLength = 3
		}
		require.Len(t, advertisedCurves, expectedLength)
	}
}
