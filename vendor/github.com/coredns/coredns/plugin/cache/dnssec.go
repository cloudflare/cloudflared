package cache

import "github.com/miekg/dns"

// filterRRSlice filters out OPT RRs, and sets all RR TTLs to ttl.
// If dup is true the RRs in rrs are _copied_ into the slice that is
// returned.
func filterRRSlice(rrs []dns.RR, ttl uint32, dup bool) []dns.RR {
	j := 0
	rs := make([]dns.RR, len(rrs))
	for _, r := range rrs {
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
