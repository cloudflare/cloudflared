package origin

import (
	"fmt"
	"net"
)

const (
	// Used to discover HA Warp servers
	srvService = "warp"
	srvProto   = "tcp"
	srvName    = "cloudflarewarp.com"
)

func ResolveEdgeIPs(addresses []string) ([]*net.TCPAddr, error) {
	if len(addresses) > 0 {
		var tcpAddrs []*net.TCPAddr
		for _, address := range addresses {
			// Addresses specified (for testing, usually)
			tcpAddr, err := net.ResolveTCPAddr("tcp", address)
			if err != nil {
				return nil, err
			}
			tcpAddrs = append(tcpAddrs, tcpAddr)
		}
		return tcpAddrs, nil
	}
	// HA service discovery lookup
	_, addrs, err := net.LookupSRV(srvService, srvProto, srvName)
	if err != nil {
		return nil, err
	}
	var resolvedIPsPerCNAME [][]*net.TCPAddr
	var lookupErr error
	for _, addr := range addrs {
		ips, err := ResolveSRVToTCP(addr)
		if err != nil || len(ips) == 0 {
			// don't return early, we might be able to resolve other addresses
			lookupErr = err
			continue
		}
		resolvedIPsPerCNAME = append(resolvedIPsPerCNAME, ips)
	}
	ips := FlattenServiceIPs(resolvedIPsPerCNAME)
	if lookupErr == nil && len(ips) == 0 {
		return nil, fmt.Errorf("Unknown service discovery error")
	}
	return ips, lookupErr
}

func ResolveSRVToTCP(srv *net.SRV) ([]*net.TCPAddr, error) {
	ips, err := net.LookupIP(srv.Target)
	if err != nil {
		return nil, err
	}
	addrs := make([]*net.TCPAddr, len(ips))
	for i, ip := range ips {
		addrs[i] = &net.TCPAddr{IP: ip, Port: int(srv.Port)}
	}
	return addrs, nil
}

// FlattenServiceIPs transposes and flattens the input slices such that the
// first element of the n inner slices are the first n elements of the result.
func FlattenServiceIPs(ipsByService [][]*net.TCPAddr) []*net.TCPAddr {
	var result []*net.TCPAddr
	for len(ipsByService) > 0 {
		filtered := ipsByService[:0]
		for _, ips := range ipsByService {
			if len(ips) == 0 {
				// sanity check
				continue
			}
			result = append(result, ips[0])
			if len(ips) > 1 {
				filtered = append(filtered, ips[1:])
			}
		}
		ipsByService = filtered
	}
	return result
}
