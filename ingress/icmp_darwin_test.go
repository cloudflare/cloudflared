//go:build darwin

package ingress

import (
	"math"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSingleEchoIDTracker(t *testing.T) {
	tracker := newEchoIDTracker()
	srcIP := netip.MustParseAddr("127.0.0.1")
	echoID, ok := tracker.get(srcIP)
	require.False(t, ok)
	require.Equal(t, uint16(0), echoID)

	// not assigned yet, so nothing to release
	require.False(t, tracker.release(srcIP, echoID))

	echoID, ok = tracker.assign(srcIP)
	require.True(t, ok)
	require.Equal(t, uint16(0), echoID)

	echoID, ok = tracker.get(srcIP)
	require.True(t, ok)
	require.Equal(t, uint16(0), echoID)

	// releasing a different ID returns false
	require.False(t, tracker.release(srcIP, 1999))
	require.True(t, tracker.release(srcIP, echoID))
	// releasing the second time returns false
	require.False(t, tracker.release(srcIP, echoID))

	echoID, ok = tracker.get(srcIP)
	require.False(t, ok)
	require.Equal(t, uint16(0), echoID)

	// Move to the next IP
	echoID, ok = tracker.assign(srcIP)
	require.True(t, ok)
	require.Equal(t, uint16(1), echoID)
}

func TestFullEchoIDTracker(t *testing.T) {
	tracker := newEchoIDTracker()
	firstIP := netip.MustParseAddr("172.16.0.1")
	srcIP := firstIP

	for i := uint16(0); i < math.MaxUint16; i++ {
		echoID, ok := tracker.assign(srcIP)
		require.True(t, ok)
		require.Equal(t, i, echoID)

		echoID, ok = tracker.get(srcIP)
		require.True(t, ok)
		require.Equal(t, i, echoID)
		srcIP = srcIP.Next()
	}

	// All echo IDs are assigned
	echoID, ok := tracker.assign(srcIP.Next())
	require.False(t, ok)
	require.Equal(t, uint16(0), echoID)

	srcIP = firstIP
	for i := uint16(0); i < math.MaxUint16; i++ {
		ok := tracker.release(srcIP, i)
		require.True(t, ok)

		echoID, ok = tracker.get(srcIP)
		require.False(t, ok)
		require.Equal(t, uint16(0), echoID)
		srcIP = srcIP.Next()
	}

	// The IDs are assignable again
	srcIP = firstIP
	for i := uint16(0); i < math.MaxUint16; i++ {
		echoID, ok := tracker.assign(srcIP)
		require.True(t, ok)
		require.Equal(t, i, echoID)

		echoID, ok = tracker.get(srcIP)
		require.True(t, ok)
		require.Equal(t, i, echoID)
		srcIP = srcIP.Next()
	}
}
