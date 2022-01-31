package cache

import "github.com/miekg/dns"

// isDNSSEC returns true if r is a DNSSEC record. NSEC,NSEC3,DS and RRSIG/SIG
// are DNSSEC records. DNSKEYs is not in this list on the assumption that the
// client explicitly asked for it.
func isDNSSEC(r dns.RR) bool {
	switch r.Header().Rrtype {
	case dns.TypeNSEC:
		return true
	case dns.TypeNSEC3:
		return true
	case dns.TypeDS:
		return true
	case dns.TypeRRSIG:
		return true
	case dns.TypeSIG:
		return true
	}
	return false
}

// filterRRSlice filters rrs and removes DNSSEC RRs when do is false. In the returned slice
// the TTLs are set to ttl. If dup is true the RRs in rrs are _copied_ into the slice that is
// returned.
func filterRRSlice(rrs []dns.RR, ttl uint32, do, dup bool) []dns.RR {
	j := 0
	rs := make([]dns.RR, len(rrs))
	for _, r := range rrs {
		if !do && isDNSSEC(r) {
			continue
		}
		if r.Header().Rrtype == dns.TypeOPT {
			continue
		}
		r.Header().Ttl = ttl
		if dup {
			rs[j] = dns.Copy(r)
		} else {
			rs[j] = r
		}
		j++
	}
	return rs[:j]
}
