package edgediscovery

import (
	"fmt"
	"math"
	"math/rand"
	"net"
	"reflect"
	"testing/quick"
)

type mockAddrs struct {
	// a set of synthetic SRV records
	addrMap map[net.SRV][]*net.TCPAddr
	// the total number of addresses, aggregated across addrMap.
	// For the convenience of test code that would otherwise have to compute
	// this by hand every time.
	numAddrs int
}

func newMockAddrs(port uint16, numRegions uint8, numAddrsPerRegion uint8) mockAddrs {
	addrMap := make(map[net.SRV][]*net.TCPAddr)
	numAddrs := 0

	for r := uint8(0); r < numRegions; r++ {
		var (
			srv   = net.SRV{Target: fmt.Sprintf("test-region-%v.example.com", r), Port: port}
			addrs []*net.TCPAddr
		)
		for a := uint8(0); a < numAddrsPerRegion; a++ {
			addrs = append(addrs, &net.TCPAddr{
				IP:   net.ParseIP(fmt.Sprintf("10.0.%v.%v", r, a)),
				Port: int(port),
			})
		}
		addrMap[srv] = addrs
		numAddrs += len(addrs)
	}
	return mockAddrs{addrMap: addrMap, numAddrs: numAddrs}
}

var _ quick.Generator = mockAddrs{}

func (mockAddrs) Generate(rand *rand.Rand, size int) reflect.Value {
	port := uint16(rand.Intn(math.MaxUint16))
	numRegions := uint8(1 + rand.Intn(10))
	numAddrsPerRegion := uint8(1 + rand.Intn(32))
	result := newMockAddrs(port, numRegions, numAddrsPerRegion)
	return reflect.ValueOf(result)
}

// Returns a function compatible with net.LookupSRV that will return the SRV
// records from mockAddrs.
func mockNetLookupSRV(
	m mockAddrs,
) func(service, proto, name string) (cname string, addrs []*net.SRV, err error) {
	var addrs []*net.SRV
	for k := range m.addrMap {
		addr := k
		addrs = append(addrs, &addr)
		// We can't just do
		//   addrs = append(addrs, &k)
		// `k` will be reused by subsequent loop iterations,
		// so all the copies of `&k` would point to the same location.
	}
	return func(_, _, _ string) (string, []*net.SRV, error) {
		return "", addrs, nil
	}
}

// Returns a function compatible with net.LookupIP that translates the SRV records
// from mockAddrs into IP addresses, based on the TCP addresses in mockAddrs.
func mockNetLookupIP(
	m mockAddrs,
) func(host string) ([]net.IP, error) {
	return func(host string) ([]net.IP, error) {
		for srv, tcpAddrs := range m.addrMap {
			if srv.Target != host {
				continue
			}
			result := make([]net.IP, len(tcpAddrs))
			for i, tcpAddr := range tcpAddrs {
				result[i] = tcpAddr.IP
			}
			return result, nil
		}
		return nil, fmt.Errorf("No IPs for %v", host)
	}
}

type mockEdgeServiceDiscoverer struct {
}

func (mr *mockEdgeServiceDiscoverer) Addr() (*net.TCPAddr, error) {
	return &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 63102,
	}, nil
}

func (mr *mockEdgeServiceDiscoverer) AnyAddr() (*net.TCPAddr, error) {
	return &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 63102,
	}, nil
}

func (mr *mockEdgeServiceDiscoverer) ReplaceAddr(addr *net.TCPAddr) {}

func (mr *mockEdgeServiceDiscoverer) MarkAddrBad(addr *net.TCPAddr) {}

func (mr *mockEdgeServiceDiscoverer) AvailableAddrs() int {
	return 1
}

func (mr *mockEdgeServiceDiscoverer) Refresh() error {
	return nil
}
