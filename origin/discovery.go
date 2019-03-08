package origin

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	// Used to discover HA Warp servers
	srvService = "warp"
	srvProto   = "tcp"
	srvName    = "cloudflarewarp.com"

	// Used to fallback to DoT when we can't use the default resolver to
	// discover HA Warp servers (GitHub issue #75).
	dotServerName = "cloudflare-dns.com"
	dotServerAddr = "1.1.1.1:853"
	dotTimeout    = time.Duration(15 * time.Second)
)

var friendlyDNSErrorLines = []string{
	`Please try the following things to diagnose this issue:`,
	`  1. ensure that cloudflarewarp.com is returning "warp" service records.`,
	`     Run your system's equivalent of: dig srv _warp._tcp.cloudflarewarp.com`,
	`  2. ensure that your DNS resolver is not returning compressed SRV records.`,
	`     See GitHub issue https://github.com/golang/go/issues/27546`,
	`     For example, you could use Cloudflare's 1.1.1.1 as your resolver:`,
	`     https://developers.cloudflare.com/1.1.1.1/setting-up-1.1.1.1/`,
}

func ResolveEdgeIPs(logger *log.Logger, addresses []string) ([]*net.TCPAddr, error) {
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
		// Try to fall back to DoT from Cloudflare directly.
		//
		// Note: Instead of DoT, we could also have used DoH. Either of these:
		//     - directly via the JSON API (https://1.1.1.1/dns-query?ct=application/dns-json&name=_warp._tcp.cloudflarewarp.com&type=srv)
		//     - indirectly via `tunneldns.NewUpstreamHTTPS()`
		// But both of these cases miss out on a key feature from the stdlib:
		//     "The returned records are sorted by priority and randomized by weight within a priority."
		//     (https://golang.org/pkg/net/#Resolver.LookupSRV)
		// Does this matter? I don't know. It may someday. Let's use DoT so we don't need to worry about it.
		// See also: Go feature request for stdlib-supported DoH: https://github.com/golang/go/issues/27552
		r := fallbackResolver(dotServerName, dotServerAddr)
		ctx, cancel := context.WithTimeout(context.Background(), dotTimeout)
		defer cancel()
		_, fallbackAddrs, fallbackErr := r.LookupSRV(ctx, srvService, srvProto, srvName)
		if fallbackErr != nil || len(fallbackAddrs) == 0 {
			// use the original DNS error `err` in messages, not `fallbackErr`
			logger.Errorln("Error looking up Cloudflare edge IPs: the DNS query failed:", err)
			for _, s := range friendlyDNSErrorLines {
				logger.Errorln(s)
			}
			return nil, errors.Wrap(err, "Could not lookup srv records on _warp._tcp.cloudflarewarp.com")
		}
		// Accept the fallback results and keep going
		addrs = fallbackAddrs
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

// Inspiration: https://github.com/artyom/dot/blob/master/dot.go
func fallbackResolver(serverName, serverAddress string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _ string, _ string) (net.Conn, error) {
			var dialer net.Dialer
			conn, err := dialer.DialContext(ctx, "tcp", serverAddress)
			if err != nil {
				return nil, err
			}
			tlsConfig := &tls.Config{ServerName: serverName}
			return tls.Client(conn, tlsConfig), nil
		},
	}
}
