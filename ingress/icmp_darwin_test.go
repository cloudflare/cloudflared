//go:build darwin

package ingress

import (
	"math"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/packet"
)

func TestSingleEchoIDTracker(t *testing.T) {
	tracker := newEchoIDTracker()
	key := flow3Tuple{
		srcIP:          netip.MustParseAddr("172.16.0.1"),
		dstIP:          netip.MustParseAddr("172.16.0.2"),
		originalEchoID: 5182,
	}

	// not assigned yet, so nothing to release
	require.False(t, tracker.release(key, 0))

	echoID, ok := tracker.getOrAssign(key)
	require.True(t, ok)
	require.Equal(t, uint16(0), echoID)

	//  Second time should return the same echo ID
	echoID, ok = tracker.getOrAssign(key)
	require.True(t, ok)
	require.Equal(t, uint16(0), echoID)

	// releasing a different ID returns false
	require.False(t, tracker.release(key, 1999))
	require.True(t, tracker.release(key, echoID))
	// releasing the second time returns false
	require.False(t, tracker.release(key, echoID))

	// Move to the next IP
	echoID, ok = tracker.getOrAssign(key)
	require.True(t, ok)
	require.Equal(t, uint16(1), echoID)
}

func TestFullEchoIDTracker(t *testing.T) {
	var (
		dstIP          = netip.MustParseAddr("192.168.0.1")
		originalEchoID = 41820
	)
	tracker := newEchoIDTracker()
	firstSrcIP := netip.MustParseAddr("172.16.0.1")
	srcIP := firstSrcIP

	for i := uint16(0); i < math.MaxUint16; i++ {
		key := flow3Tuple{
			srcIP:          srcIP,
			dstIP:          dstIP,
			originalEchoID: originalEchoID,
		}
		echoID, ok := tracker.getOrAssign(key)
		require.True(t, ok)
		require.Equal(t, i, echoID)

		echoID, ok = tracker.get(key)
		require.True(t, ok)
		require.Equal(t, i, echoID)

		srcIP = srcIP.Next()
	}

	key := flow3Tuple{
		srcIP:          srcIP.Next(),
		dstIP:          dstIP,
		originalEchoID: originalEchoID,
	}
	// All echo IDs are assigned
	echoID, ok := tracker.getOrAssign(key)
	require.False(t, ok)
	require.Equal(t, uint16(0), echoID)

	srcIP = firstSrcIP
	for i := uint16(0); i < math.MaxUint16; i++ {
		key := flow3Tuple{
			srcIP:          srcIP,
			dstIP:          dstIP,
			originalEchoID: originalEchoID,
		}
		ok := tracker.release(key, i)
		require.True(t, ok)

		echoID, ok = tracker.get(key)
		require.False(t, ok)
		require.Equal(t, uint16(0), echoID)
		srcIP = srcIP.Next()
	}

	// The IDs are assignable again
	srcIP = firstSrcIP
	for i := uint16(0); i < math.MaxUint16; i++ {
		key := flow3Tuple{
			srcIP:          srcIP,
			dstIP:          dstIP,
			originalEchoID: originalEchoID,
		}
		echoID, ok := tracker.getOrAssign(key)
		require.True(t, ok)
		require.Equal(t, i, echoID)

		echoID, ok = tracker.get(key)
		require.True(t, ok)
		require.Equal(t, i, echoID)
		srcIP = srcIP.Next()
	}
}

func (eit *echoIDTracker) get(key flow3Tuple) (id uint16, exist bool) {
	eit.lock.Lock()
	defer eit.lock.Unlock()
	id, exists := eit.mapping[key]
	return id, exists
}

func getFunnel(t *testing.T, proxy *icmpProxy, tuple flow3Tuple) (packet.Funnel, bool) {
	assignedEchoID, success := proxy.echoIDTracker.getOrAssign(tuple)
	require.True(t, success)
	return proxy.srcFunnelTracker.Get(echoFunnelID(assignedEchoID))
}
