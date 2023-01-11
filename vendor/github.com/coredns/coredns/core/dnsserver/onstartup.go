package dnsserver

import (
	"fmt"
	"regexp"
	"sort"

	"github.com/coredns/coredns/plugin/pkg/dnsutil"
)

// checkZoneSyntax() checks whether the given string match 1035 Preferred Syntax or not.
// The root zone, and all reverse zones always return true even though they technically don't meet 1035 Preferred Syntax
func checkZoneSyntax(zone string) bool {
	if zone == "." || dnsutil.IsReverse(zone) != 0 {
		return true
	}
	regex1035PreferredSyntax, _ := regexp.MatchString(`^(([A-Za-z]([A-Za-z0-9-]*[A-Za-z0-9])?)\.)+$`, zone)
	return regex1035PreferredSyntax
}

// startUpZones creates the text that we show when starting up:
// grpc://example.com.:1055
// example.com.:1053 on 127.0.0.1
func startUpZones(protocol, addr string, zones map[string][]*Config) string {
	s := ""

	keys := make([]string, len(zones))
	i := 0

	for k := range zones {
		keys[i] = k
		i++
	}
	sort.Strings(keys)

	for _, zone := range keys {
		if !checkZoneSyntax(zone) {
			s += fmt.Sprintf("Warning: Domain %q does not follow RFC1035 preferred syntax\n", zone)
		}
		// split addr into protocol, IP and Port
		_, ip, port, err := SplitProtocolHostPort(addr)

		if err != nil {
			// this should not happen, but we need to take care of it anyway
			s += fmt.Sprintln(protocol + zone + ":" + addr)
			continue
		}
		if ip == "" {
			s += fmt.Sprintln(protocol + zone + ":" + port)
			continue
		}
		// if the server is listening on a specific address let's make it visible in the log,
		// so one can differentiate between all active listeners
		s += fmt.Sprintln(protocol + zone + ":" + port + " on " + ip)
	}
	return s
}
