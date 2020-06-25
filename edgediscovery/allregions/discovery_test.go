package allregions

import (
	"testing"

	"github.com/cloudflare/cloudflared/logger"
	"github.com/stretchr/testify/assert"
)

func TestEdgeDiscovery(t *testing.T) {
	mockAddrs := newMockAddrs(19, 2, 5)
	netLookupSRV = mockNetLookupSRV(mockAddrs)
	netLookupIP = mockNetLookupIP(mockAddrs)

	expectedAddrSet := map[string]bool{}
	for _, addrs := range mockAddrs.addrMap {
		for _, addr := range addrs {
			expectedAddrSet[addr.String()] = true
		}
	}

	l := logger.NewOutputWriter(logger.NewMockWriteManager())
	addrLists, err := edgeDiscovery(l)
	assert.NoError(t, err)
	actualAddrSet := map[string]bool{}
	for _, addrs := range addrLists {
		for _, addr := range addrs {
			actualAddrSet[addr.String()] = true
		}
	}

	assert.Equal(t, expectedAddrSet, actualAddrSet)
}
