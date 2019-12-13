package connection

import (
	"net"
	"sync"
	"testing"
	"testing/quick"
	"time"

	"github.com/sirupsen/logrus"
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

	addrLists, err := EdgeDiscovery(logrus.New().WithFields(logrus.Fields{}))
	assert.NoError(t, err)
	actualAddrSet := map[string]bool{}
	for _, addrs := range addrLists {
		for _, addr := range addrs {
			actualAddrSet[addr.String()] = true
		}
	}

	assert.Equal(t, expectedAddrSet, actualAddrSet)
}

func TestAllInUse(t *testing.T) {
	for _, testCase := range []struct {
		regions  []*region
		expected map[string]*net.TCPAddr
	}{
		{
			regions:  nil,
			expected: map[string]*net.TCPAddr{},
		},
		{
			regions: []*region{
				&region{inUse: map[string]*net.TCPAddr{}},
				&region{inUse: map[string]*net.TCPAddr{}},
			},
			expected: map[string]*net.TCPAddr{},
		},
		{
			regions: []*region{
				&region{inUse: map[string]*net.TCPAddr{":1": &net.TCPAddr{Port: 1}}},
				&region{inUse: map[string]*net.TCPAddr{":4": &net.TCPAddr{Port: 4}}},
			},
			expected: map[string]*net.TCPAddr{":1": &net.TCPAddr{Port: 1}, ":4": &net.TCPAddr{Port: 4}},
		},
	} {
		actual := allInUse(testCase.regions)
		assert.Equal(t, testCase.expected, actual)
	}
}

func TestMakeRegions(t *testing.T) {
	for _, testCase := range []struct {
		addrList [][]*net.TCPAddr
		inUse    map[string]*net.TCPAddr
		expected []*region
	}{
		{
			addrList: [][]*net.TCPAddr{},
			expected: nil,
		},
		{
			addrList: [][]*net.TCPAddr{
				[]*net.TCPAddr{&net.TCPAddr{Port: 1}, &net.TCPAddr{Port: 2}},
			},
			expected: []*region{
				&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 1}, &net.TCPAddr{Port: 2}}, inUse: map[string]*net.TCPAddr{}},
			},
		},
		{
			addrList: [][]*net.TCPAddr{
				[]*net.TCPAddr{&net.TCPAddr{Port: 1}, &net.TCPAddr{Port: 2}},
				[]*net.TCPAddr{&net.TCPAddr{Port: 3}, &net.TCPAddr{Port: 4}},
			},
			expected: []*region{
				&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 1}, &net.TCPAddr{Port: 2}}, inUse: map[string]*net.TCPAddr{}},
				&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 3}, &net.TCPAddr{Port: 4}}, inUse: map[string]*net.TCPAddr{}},
			},
		},
		{
			addrList: [][]*net.TCPAddr{
				[]*net.TCPAddr{&net.TCPAddr{Port: 1}, &net.TCPAddr{Port: 2}},
				[]*net.TCPAddr{&net.TCPAddr{Port: 3}, &net.TCPAddr{Port: 4}},
			},
			inUse: map[string]*net.TCPAddr{
				":1": &net.TCPAddr{Port: 1},
				":4": &net.TCPAddr{Port: 4},
			},
			expected: []*region{
				&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 2}}, inUse: map[string]*net.TCPAddr{":1": &net.TCPAddr{Port: 1}}},
				&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 3}}, inUse: map[string]*net.TCPAddr{":4": &net.TCPAddr{Port: 4}}},
			},
		},
	} {
		actual := makeHARegions(testCase.addrList, testCase.inUse)
		assert.Equal(t, testCase.expected, actual)
	}
}

func assertIsBalanced(t *testing.T, regions []*region) bool {
	// Compute max(len(region.addrs) for region in regions)
	// No region should have significantly fewer addresses than this
	var longestAddrs int
	{
		longestAddrs = 0
		for _, region := range regions {
			if l := len(region.addrs); l > longestAddrs {
				longestAddrs = l
			}
		}
	}
	for _, region := range regions {
		if len(region.addrs) == longestAddrs || len(region.addrs) == longestAddrs-1 {
			continue
		}
		return assert.Fail(t,
			"found a region with %v free addrs, while the longest addrs list is %v",
			len(region.addrs), longestAddrs)
	}
	return true
}

// Various end-to-end tests, run with quickcheck (i.e. the testing/quick package)
func TestEdgeAddrResolver(t *testing.T) {
	concurrentReplacement := func(mockAddrs mockAddrs) bool {
		netLookupSRV = mockNetLookupSRV(mockAddrs)
		netLookupIP = mockNetLookupIP(mockAddrs)

		resolver, err := NewEdgeAddrResolver(logrus.New())
		if !assert.NoError(t, err) {
			return false
		}
		assert.Equal(t, mockAddrs.numAddrs, resolver.AvailableAddrs(),
			"every address should be initially available")

		// Create several goroutines to simulate HA connections that acquire
		// and replace IP addresses.
		var wg sync.WaitGroup
		wg.Add(mockAddrs.numAddrs)
		for i := 0; i < mockAddrs.numAddrs; i++ {
			go func() {
				defer wg.Done()
				const reconnectionCount = 50
				for i := 0; i < reconnectionCount; i++ {
					if resolver.AvailableAddrs() == 0 {
						err = resolver.Refresh()
						assert.NoError(t, err)
					}
					addr, err := resolver.Addr()
					if !assert.NoError(t, err) {
						return
					}
					time.Sleep(0) // allow some other goroutine to run
					resolver.ReplaceAddr(addr)
					time.Sleep(0) // allow some other goroutine to run
				}
			}()
		}
		wg.Wait()
		assert.Equal(t, mockAddrs.numAddrs, resolver.AvailableAddrs(),
			"every address should be available after replacement")
		return !t.Failed()
	}

	badAddrWithRefresh := func(mockAddrs mockAddrs) bool {
		netLookupSRV = mockNetLookupSRV(mockAddrs)
		netLookupIP = mockNetLookupIP(mockAddrs)

		resolver, err := NewEdgeAddrResolver(logrus.New())
		if !assert.NoError(t, err) {
			return false
		}
		assert.Equal(t, mockAddrs.numAddrs, resolver.AvailableAddrs(),
			"every address should be initially available")

		var addrs []*net.TCPAddr
		for i := 0; i < mockAddrs.numAddrs; i++ {
			assert.Equal(t, mockAddrs.numAddrs-i, resolver.AvailableAddrs())
			addr, err := resolver.Addr()
			assert.NoError(t, err)
			addrs = append(addrs, addr)
		}
		assert.Equal(t, 0, resolver.AvailableAddrs(), "all addresses should have been taken")
		_, err = resolver.Addr()
		assert.Error(t, err)

		anyAddr, err := resolver.AnyAddr()
		assert.NoError(t, err, "should still be okay to call AnyAddr")

		resolver.MarkAddrBad(anyAddr)

		assert.Equal(t, 0, resolver.AvailableAddrs(), "all addresses should still be used")
		_, err = resolver.Addr()
		assert.Error(t, err, "all addresses should still be used")

		err = resolver.Refresh()
		assert.NoError(t, err, "Refresh() should have worked")

		assert.Equal(t, 1, resolver.AvailableAddrs(),
			"Refresh() should have reset the state of the 'bad' address")
		addr, err := resolver.Addr()
		assert.NoError(t, err)
		assert.Equal(t, anyAddr, addr)

		_, err = resolver.Addr()
		assert.Error(t, err, "all addresses should be used again")

		return !t.Failed()
	}

	assert.NoError(t, quick.Check(concurrentReplacement, nil))
	assert.NoError(t, quick.Check(badAddrWithRefresh, nil))
}

// "White-box" test: runs Addr() and checks internal state
func TestEdgeAddrResolver_Addr(t *testing.T) {
	e := &EdgeAddrResolver{regions: nil}
	addr, err := e.Addr()
	assert.Error(t, err)

	testRegions := func() []*region {
		return []*region{
			&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 1}}, inUse: map[string]*net.TCPAddr{":2": &net.TCPAddr{Port: 2}, ":3": &net.TCPAddr{Port: 3}}},
			&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 4}, &net.TCPAddr{Port: 5}}, inUse: map[string]*net.TCPAddr{":6": &net.TCPAddr{Port: 6}}},
			&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 7}, &net.TCPAddr{Port: 8}}, inUse: map[string]*net.TCPAddr{":9": &net.TCPAddr{Port: 9}}},
		}
	}
	e = &EdgeAddrResolver{regions: testRegions()}
	addr, err = e.Addr()
	assert.NoError(t, err)
	assert.Equal(t, &net.TCPAddr{Port: 4}, addr)
	var expected []*region
	{
		expected = testRegions()
		expected[1].addrs = expected[1].addrs[1:]
		expected[1].inUse[":4"] = &net.TCPAddr{Port: 4}
	}
	assert.Equal(t, expected, e.regions)
}

// "White-box" test: runs AnyAddr() and checks internal state
func TestEdgeAddrResolver_AnyAddr(t *testing.T) {
	e := &EdgeAddrResolver{regions: nil}
	addr, err := e.AnyAddr()
	assert.Error(t, err)

	e = &EdgeAddrResolver{regions: []*region{&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 1}}, inUse: map[string]*net.TCPAddr{":2": &net.TCPAddr{Port: 2}}}}}
	addr, err = e.AnyAddr()
	assert.NoError(t, err)
	assert.Equal(t, &net.TCPAddr{Port: 1}, addr, "should have chosen the inactive address")

	e = &EdgeAddrResolver{regions: []*region{&region{inUse: map[string]*net.TCPAddr{":1": &net.TCPAddr{Port: 1}}}}}
	addr, err = e.AnyAddr()
	assert.NoError(t, err)
	assert.Equal(t, &net.TCPAddr{Port: 1}, addr, "should have chosen an active address rather than nothing")
}

// "White-box" test: runs ReplaceAddr() and checks internal state
func TestEdgeAddrResolver_ReplaceAddr(t *testing.T) {
	e := &EdgeAddrResolver{regions: nil}
	e.ReplaceAddr(&net.TCPAddr{Port: 1}) // this shouldn't panic, I guess

	testRegions := func() []*region {
		return []*region{
			&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 1}}, inUse: map[string]*net.TCPAddr{":2": &net.TCPAddr{Port: 2}, ":3": &net.TCPAddr{Port: 3}}},
			&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 4}, &net.TCPAddr{Port: 5}}, inUse: map[string]*net.TCPAddr{":6": &net.TCPAddr{Port: 6}}},
			&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 7}, &net.TCPAddr{Port: 8}}, inUse: map[string]*net.TCPAddr{":9": &net.TCPAddr{Port: 9}}},
		}
	}
	e = &EdgeAddrResolver{regions: testRegions()}
	e.ReplaceAddr(&net.TCPAddr{Port: 6})
	var expected []*region
	{
		expected = testRegions()
		delete(expected[1].inUse, ":6")
		expected[1].addrs = append(expected[1].addrs, &net.TCPAddr{Port: 6})
	}
	assert.Equal(t, expected, e.regions)
}

// "White-box" test: runs MarkAddrBad() and checks internal state
func TestEdgeAddrResolver_MarkAddrBad(t *testing.T) {
	e := &EdgeAddrResolver{regions: nil}
	e.ReplaceAddr(&net.TCPAddr{Port: 1}) // this shouldn't panic, I guess

	testRegions := func() []*region {
		return []*region{
			&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 1}}, inUse: map[string]*net.TCPAddr{":2": &net.TCPAddr{Port: 2}, ":3": &net.TCPAddr{Port: 3}}},
			&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 4}, &net.TCPAddr{Port: 5}}, inUse: map[string]*net.TCPAddr{":6": &net.TCPAddr{Port: 6}}},
			&region{addrs: []*net.TCPAddr{&net.TCPAddr{Port: 7}, &net.TCPAddr{Port: 8}}, inUse: map[string]*net.TCPAddr{":9": &net.TCPAddr{Port: 9}}},
		}
	}
	e = &EdgeAddrResolver{regions: testRegions()}
	e.MarkAddrBad(&net.TCPAddr{Port: 6})
	var expected []*region
	{
		expected = testRegions()
		delete(expected[1].inUse, ":6")
		expected[1].bad = append(expected[1].bad, &net.TCPAddr{Port: 6})
	}
	assert.Equal(t, expected, e.regions)
}
