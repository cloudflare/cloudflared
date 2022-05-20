package allregions

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

const (
	// Used to discover HA origintunneld servers
	srvService = "v2-origintunneld"
	srvProto   = "tcp"
	srvName    = "argotunnel.com"

	// Used to fallback to DoT when we can't use the default resolver to
	// discover HA origintunneld servers (GitHub issue #75).
	dotServerName = "cloudflare-dns.com"
	dotServerAddr = "1.1.1.1:853"
	dotTimeout    = 15 * time.Second

	logFieldAddress = "address"
)

// Redeclare network functions so they can be overridden in tests.
var (
	netLookupSRV = net.LookupSRV
	netLookupIP  = net.LookupIP
)

// ConfigIPVersion is the selection of IP versions from config
type ConfigIPVersion int8

const (
	Auto     ConfigIPVersion = 2
	IPv4Only ConfigIPVersion = 4
	IPv6Only ConfigIPVersion = 6
)

// IPVersion is the IP version of an EdgeAddr
type EdgeIPVersion int8

const (
	V4 EdgeIPVersion = 4
	V6 EdgeIPVersion = 6
)

// String returns the enum's constant name.
func (c EdgeIPVersion) String() string {
	switch c {
	case V4:
		return "4"
	case V6:
		return "6"
	default:
		return ""
	}
}

// EdgeAddr is a representation of possible ways to refer an edge location.
type EdgeAddr struct {
	TCP       *net.TCPAddr
	UDP       *net.UDPAddr
	IPVersion EdgeIPVersion
}

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

// EdgeDiscovery implements HA service discovery lookup.
func edgeDiscovery(log *zerolog.Logger, srvService string) ([][]*EdgeAddr, error) {
	log.Debug().Str("domain", "_"+srvService+"._"+srvProto+"."+srvName).Msg("looking up edge SRV record")

	_, addrs, err := netLookupSRV(srvService, srvProto, srvName)
	if err != nil {
		_, fallbackAddrs, fallbackErr := fallbackLookupSRV(srvService, srvProto, srvName)
		if fallbackErr != nil || len(fallbackAddrs) == 0 {
			// use the original DNS error `err` in messages, not `fallbackErr`
			log.Err(err).Msg("Error looking up Cloudflare edge IPs: the DNS query failed")
			for _, s := range friendlyDNSErrorLines {
				log.Error().Msg(s)
			}
			return nil, errors.Wrapf(err, "Could not lookup srv records on _%v._%v.%v", srvService, srvProto, srvName)
		}
		// Accept the fallback results and keep going
		addrs = fallbackAddrs
	}

	var resolvedAddrPerCNAME [][]*EdgeAddr
	for _, addr := range addrs {
		edgeAddrs, err := resolveSRV(addr)
		if err != nil {
			return nil, err
		}
		for _, e := range edgeAddrs {
			log.Debug().Msgf("Edge Address: %+v", *e)
		}
		resolvedAddrPerCNAME = append(resolvedAddrPerCNAME, edgeAddrs)
	}

	return resolvedAddrPerCNAME, nil
}

func lookupSRVWithDOT(srvService string, srvProto string, srvName string) (cname string, addrs []*net.SRV, err error) {
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

func resolveSRV(srv *net.SRV) ([]*EdgeAddr, error) {
	ips, err := netLookupIP(srv.Target)
	if err != nil {
		return nil, errors.Wrapf(err, "Couldn't resolve SRV record %v", srv)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("SRV record %v had no IPs", srv)
	}
	addrs := make([]*EdgeAddr, len(ips))
	for i, ip := range ips {
		version := V6
		if ip.To4() != nil {
			version = V4
		}
		addrs[i] = &EdgeAddr{
			TCP:       &net.TCPAddr{IP: ip, Port: int(srv.Port)},
			UDP:       &net.UDPAddr{IP: ip, Port: int(srv.Port)},
			IPVersion: version,
		}
	}
	return addrs, nil
}

// ResolveAddrs resolves TCP address given a list of addresses. Address can be a hostname, however, it will return at most one
// of the hostname's IP addresses.
func ResolveAddrs(addrs []string, log *zerolog.Logger) (resolved []*EdgeAddr) {
	for _, addr := range addrs {
		tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			log.Error().Str(logFieldAddress, addr).Err(err).Msg("failed to resolve to TCP address")
			continue
		}

		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			log.Error().Str(logFieldAddress, addr).Err(err).Msg("failed to resolve to UDP address")
			continue
		}
		version := V6
		if udpAddr.IP.To4() != nil {
			version = V4
		}
		resolved = append(resolved, &EdgeAddr{
			TCP:       tcpAddr,
			UDP:       udpAddr,
			IPVersion: version,
		})
	}
	return
}
