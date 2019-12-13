package connection

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// Used to discover HA origintunneld servers
	srvService = "origintunneld"
	srvProto   = "tcp"
	srvName    = "argotunnel.com"

	// Used to fallback to DoT when we can't use the default resolver to
	// discover HA origintunneld servers (GitHub issue #75).
	dotServerName = "cloudflare-dns.com"
	dotServerAddr = "1.1.1.1:853"
	dotTimeout    = time.Duration(15 * time.Second)

	// SRV record resolution TTL
	resolveEdgeAddrTTL = 1 * time.Hour

	subsystemEdgeAddrResolver = "edgeAddrResolver"
)

// Redeclare network functions so they can be overridden in tests.
var (
	netLookupSRV = net.LookupSRV
	netLookupIP  = net.LookupIP
)

// If the call to net.LookupSRV fails, try to fall back to DoT from Cloudflare directly.
//
// Note: Instead of DoT, we could also have used DoH. Either of these:
//     - directly via the JSON API (https://1.1.1.1/dns-query?ct=application/dns-json&name=_origintunneld._tcp.argotunnel.com&type=srv)
//     - indirectly via `tunneldns.NewUpstreamHTTPS()`
// But both of these cases miss out on a key feature from the stdlib:
//     "The returned records are sorted by priority and randomized by weight within a priority."
//     (https://golang.org/pkg/net/#Resolver.LookupSRV)
// Does this matter? I don't know. It may someday. Let's use DoT so we don't need to worry about it.
// See also: Go feature request for stdlib-supported DoH: https://github.com/golang/go/issues/27552
var fallbackLookupSRV = lookupSRVWithDOT

var friendlyDNSErrorLines = []string{
	`Please try the following things to diagnose this issue:`,
	`  1. ensure that argotunnel.com is returning "origintunneld" service records.`,
	`     Run your system's equivalent of: dig srv _origintunneld._tcp.argotunnel.com`,
	`  2. ensure that your DNS resolver is not returning compressed SRV records.`,
	`     See GitHub issue https://github.com/golang/go/issues/27546`,
	`     For example, you could use Cloudflare's 1.1.1.1 as your resolver:`,
	`     https://developers.cloudflare.com/1.1.1.1/setting-up-1.1.1.1/`,
}

// EdgeServiceDiscoverer is an interface for looking up Cloudflare's edge network addresses
type EdgeServiceDiscoverer interface {
	// Addr returns an unused address to connect to cloudflare's edge network.
	// Before this method returns, the address will be removed from the pool of available addresses,
	// so the caller can assume they have exclusive access to the address for tunneling purposes.
	// The caller should remember to put it back via ReplaceAddr or MarkAddrBad.
	Addr() (*net.TCPAddr, error)
	// AnyAddr returns an address to connect to cloudflare's edge network.
	// It may or may not be in active use for a tunnel.
	// The caller should NOT return it via ReplaceAddr or MarkAddrBad!
	AnyAddr() (*net.TCPAddr, error)
	// ReplaceAddr is called when the address is no longer needed, e.g. due to a scaling-down of numHAConnections.
	// It returns the address to the pool of available addresses.
	ReplaceAddr(addr *net.TCPAddr)
	// MarkAddrBad is called when there was a connectivity error for the address.
	// It marks the address as unused but doesn't return it to the pool of available addresses.
	MarkAddrBad(addr *net.TCPAddr)
	// AvailableAddrs returns the number of addresses available for use
	// (less those that have been marked bad).
	AvailableAddrs() int
	// Refresh rediscovers Cloudflare's edge network addresses.
	// It resets the state of "bad" addresses but not those in active use.
	Refresh() error
}

// EdgeAddrResolver discovers the addresses of Cloudflare's edge network through SRV record.
// It implements EdgeServiceDiscoverer interface
type EdgeAddrResolver struct {
	sync.Mutex
	// HA regions
	regions []*region
	// Logger for noteworthy events
	logger *logrus.Entry
}

type region struct {
	// Addresses that we expect will be in active use
	addrs []*net.TCPAddr
	// Addresses that are in active use.
	// This is actually a set of net.TCPAddr's, but we can't make a map like
	//     map[net.TCPAddr]bool
	// since net.TCPAddr contains a field of type net.IP and therefore it cannot be used as a map key.
	// So instead we use map[string]*net.TCPAddr, where the keys are obtained by net.TCPAddr.String().
	// (We keep the "raw" *net.TCPAddr values for the convenience of AnyAddr(). If that method didn't
	// exist, we wouldn't strictly need the values, and this could be a map[string]bool.)
	inUse map[string]*net.TCPAddr
	// Addresses that were discarded due to a network error.
	// Not sure what we'll do with these, but it feels good to keep them around for now.
	bad []*net.TCPAddr
}

func NewEdgeAddrResolver(logger *logrus.Logger) (EdgeServiceDiscoverer, error) {
	r := &EdgeAddrResolver{
		logger: logger.WithField("subsystem", subsystemEdgeAddrResolver),
	}
	if err := r.Refresh(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *EdgeAddrResolver) Addr() (*net.TCPAddr, error) {
	r.Lock()
	defer r.Unlock()

	// compute the largest region based on len(addrs)
	var largestRegion *region
	{
		if len(r.regions) == 0 {
			return nil, errors.New("No HA regions")
		}
		largestRegion = r.regions[0]
		for _, region := range r.regions[1:] {
			if len(region.addrs) > len(largestRegion.addrs) {
				largestRegion = region
			}
		}
		if len(largestRegion.addrs) == 0 {
			return nil, errors.New("No IP address to claim")
		}
	}

	var addr *net.TCPAddr
	addr, largestRegion.addrs = popAddr(largestRegion.addrs)
	largestRegion.inUse[addr.String()] = addr
	return addr, nil
}

func (r *EdgeAddrResolver) AnyAddr() (*net.TCPAddr, error) {
	r.Lock()
	defer r.Unlock()
	for _, region := range r.regions {
		// return an unused addr
		if len(region.addrs) > 0 {
			return region.addrs[rand.Intn(len(region.addrs))], nil
		}
		// return an addr that's in use
		for _, addr := range region.inUse {
			return addr, nil
		}
	}
	return nil, fmt.Errorf("No IP addresses")
}

func (r *EdgeAddrResolver) ReplaceAddr(addr *net.TCPAddr) {
	r.Lock()
	defer r.Unlock()
	addrString := addr.String()
	for _, region := range r.regions {
		if _, ok := region.inUse[addrString]; ok {
			delete(region.inUse, addrString)
			region.addrs = append(region.addrs, addr)
			break
		}
	}
}

func (r *EdgeAddrResolver) MarkAddrBad(addr *net.TCPAddr) {
	r.Lock()
	defer r.Unlock()
	addrString := addr.String()
	for _, region := range r.regions {
		if _, ok := region.inUse[addrString]; ok {
			delete(region.inUse, addrString)
			region.bad = append(region.bad, addr)
			break
		}
	}
}

func (r *EdgeAddrResolver) AvailableAddrs() int {
	r.Lock()
	defer r.Unlock()
	result := 0
	for _, region := range r.regions {
		result += len(region.addrs)
	}
	return result
}

func (r *EdgeAddrResolver) Refresh() error {
	addrLists, err := EdgeDiscovery(r.logger)
	if err != nil {
		return err
	}

	r.Lock()
	defer r.Unlock()
	inUse := allInUse(r.regions)
	r.regions = makeHARegions(addrLists, inUse)
	return nil
}

// EdgeDiscovery implements HA service discovery lookup.
func EdgeDiscovery(logger *logrus.Entry) ([][]*net.TCPAddr, error) {
	_, addrs, err := netLookupSRV(srvService, srvProto, srvName)
	if err != nil {
		_, fallbackAddrs, fallbackErr := fallbackLookupSRV(srvService, srvProto, srvName)
		if fallbackErr != nil || len(fallbackAddrs) == 0 {
			// use the original DNS error `err` in messages, not `fallbackErr`
			logger.Errorln("Error looking up Cloudflare edge IPs: the DNS query failed:", err)
			for _, s := range friendlyDNSErrorLines {
				logger.Errorln(s)
			}
			return nil, errors.Wrapf(err, "Could not lookup srv records on _%v._%v.%v", srvService, srvProto, srvName)
		}
		// Accept the fallback results and keep going
		addrs = fallbackAddrs
	}

	var resolvedIPsPerCNAME [][]*net.TCPAddr
	for _, addr := range addrs {
		ips, err := resolveSRVToTCP(addr)
		if err != nil {
			return nil, err
		}
		resolvedIPsPerCNAME = append(resolvedIPsPerCNAME, ips)
	}

	return resolvedIPsPerCNAME, nil
}

func lookupSRVWithDOT(service, proto, name string) (cname string, addrs []*net.SRV, err error) {
	// Inspiration: https://github.com/artyom/dot/blob/master/dot.go
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _ string, _ string) (net.Conn, error) {
			var dialer net.Dialer
			conn, err := dialer.DialContext(ctx, "tcp", dotServerAddr)
			if err != nil {
				return nil, err
			}
			tlsConfig := &tls.Config{ServerName: dotServerName}
			return tls.Client(conn, tlsConfig), nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), dotTimeout)
	defer cancel()
	return r.LookupSRV(ctx, srvService, srvProto, srvName)
}

func resolveSRVToTCP(srv *net.SRV) ([]*net.TCPAddr, error) {
	ips, err := netLookupIP(srv.Target)
	if err != nil {
		return nil, errors.Wrapf(err, "Couldn't resolve SRV record %v", srv)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("SRV record %v had no IPs", srv)
	}
	addrs := make([]*net.TCPAddr, len(ips))
	for i, ip := range ips {
		addrs[i] = &net.TCPAddr{IP: ip, Port: int(srv.Port)}
	}
	return addrs, nil
}

// EdgeHostnameResolver discovers the addresses of Cloudflare's edge network via a list of server hostnames.
// It implements EdgeServiceDiscoverer interface, and is used mainly for testing connectivity.
type EdgeHostnameResolver struct {
	sync.Mutex
	// hostnames of edge servers
	hostnames []string
	// Addrs to connect to cloudflare's edge network
	addrs []*net.TCPAddr
	// Addresses that are in active use.
	// This is actually a set of net.TCPAddr's. We have to encode the keys
	// with .String(), since net.TCPAddr contains a field of type net.IP and
	// therefore it cannot be used as a map key
	inUse map[string]*net.TCPAddr
	// Addresses that were discarded due to a network error.
	// Not sure what we'll do with these, but it feels good to keep them around for now.
	bad []*net.TCPAddr
}

func NewEdgeHostnameResolver(edgeHostnames []string) (EdgeServiceDiscoverer, error) {
	r := &EdgeHostnameResolver{
		hostnames: edgeHostnames,
		inUse:     map[string]*net.TCPAddr{},
	}
	if err := r.Refresh(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *EdgeHostnameResolver) Addr() (*net.TCPAddr, error) {
	r.Lock()
	defer r.Unlock()
	if len(r.addrs) == 0 {
		return nil, errors.New("No IP address to claim")
	}
	var addr *net.TCPAddr
	addr, r.addrs = popAddr(r.addrs)
	r.inUse[addr.String()] = addr
	return addr, nil
}

func (r *EdgeHostnameResolver) AnyAddr() (*net.TCPAddr, error) {
	r.Lock()
	defer r.Unlock()
	// return an unused addr
	if len(r.addrs) > 0 {
		return r.addrs[rand.Intn(len(r.addrs))], nil
	}
	// return an addr that's in use
	for _, addr := range r.inUse {
		return addr, nil
	}
	return nil, errors.New("No IP addresses")
}

func (r *EdgeHostnameResolver) ReplaceAddr(addr *net.TCPAddr) {
	r.Lock()
	defer r.Unlock()
	delete(r.inUse, addr.String())
	r.addrs = append(r.addrs, addr)
}
func (r *EdgeHostnameResolver) MarkAddrBad(addr *net.TCPAddr) {
	r.Lock()
	defer r.Unlock()
	delete(r.inUse, addr.String())
	r.bad = append(r.bad, addr)
}

func (r *EdgeHostnameResolver) AvailableAddrs() int {
	r.Lock()
	defer r.Unlock()
	return len(r.addrs)
}

func (r *EdgeHostnameResolver) Refresh() error {
	newAddrs, err := ResolveAddrs(r.hostnames)
	if err != nil {
		return err
	}
	r.Lock()
	defer r.Unlock()
	var notInUse []*net.TCPAddr
	for _, newAddr := range newAddrs {
		if _, ok := r.inUse[newAddr.String()]; !ok {
			notInUse = append(notInUse, newAddr)
		}
	}
	r.addrs = notInUse
	r.bad = nil
	return nil
}

// Resolve TCP address given a list of addresses. Address can be a hostname, however, it will return at most one
// of the hostname's IP addresses
func ResolveAddrs(addrs []string) ([]*net.TCPAddr, error) {
	var tcpAddrs []*net.TCPAddr
	for _, addr := range addrs {
		tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			return nil, err
		}
		tcpAddrs = append(tcpAddrs, tcpAddr)
	}
	return tcpAddrs, nil
}

// Compute total set of IP addresses in use. This is useful if the regions
// are returned in a different order, or if an IP address is assigned to
// a different region for some reasion.
func allInUse(regions []*region) map[string]*net.TCPAddr {
	result := make(map[string]*net.TCPAddr)
	for _, region := range regions {
		for k, v := range region.inUse {
			result[k] = v
		}
	}
	return result
}

func makeHARegions(addrLists [][]*net.TCPAddr, inUse map[string]*net.TCPAddr) (regions []*region) {
	for _, addrList := range addrLists {
		region := &region{inUse: map[string]*net.TCPAddr{}}
		for _, addr := range addrList {
			addrString := addr.String()
			// No matter what region `addr` used to belong to, it's now a part
			// of this region, so add it to this region's `inUse` map.
			if _, ok := inUse[addrString]; ok {
				region.inUse[addrString] = addr
			} else {
				region.addrs = append(region.addrs, addr)
			}
		}
		regions = append(regions, region)
	}
	return
}

func popAddr(addrs []*net.TCPAddr) (*net.TCPAddr, []*net.TCPAddr) {
	first := addrs[0]
	addrs[0] = nil // prevent memory leak
	addrs = addrs[1:]
	return first, addrs
}
