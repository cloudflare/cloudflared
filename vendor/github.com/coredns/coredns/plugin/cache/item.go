package cache

import (
	"strings"
	"time"

	"github.com/coredns/coredns/plugin/cache/freq"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

type item struct {
	Name               string
	QType              uint16
	Rcode              int
	AuthenticatedData  bool
	RecursionAvailable bool
	Answer             []dns.RR
	Ns                 []dns.RR
	Extra              []dns.RR
	wildcard           string

	origTTL uint32
	stored  time.Time

	*freq.Freq
}

func newItem(m *dns.Msg, now time.Time, d time.Duration) *item {
	i := new(item)
	if len(m.Question) != 0 {
		i.Name = m.Question[0].Name
		i.QType = m.Question[0].Qtype
	}
	i.Rcode = m.Rcode
	i.AuthenticatedData = m.AuthenticatedData
	i.RecursionAvailable = m.RecursionAvailable
	i.Answer = m.Answer
	i.Ns = m.Ns
	i.Extra = make([]dns.RR, len(m.Extra))
	// Don't copy OPT records as these are hop-by-hop.
	j := 0
	for _, e := range m.Extra {
		if e.Header().Rrtype == dns.TypeOPT {
			continue
		}
		i.Extra[j] = e
		j++
	}
	i.Extra = i.Extra[:j]

	i.origTTL = uint32(d.Seconds())
	i.stored = now.UTC()

	i.Freq = new(freq.Freq)

	return i
}

// toMsg turns i into a message, it tailors the reply to m.
// The Authoritative bit should be set to 0, but some client stub resolver implementations, most notably,
// on some legacy systems(e.g. ubuntu 14.04 with glib version 2.20), low-level glibc function `getaddrinfo`
// useb by Python/Ruby/etc.. will discard answers that do not have this bit set.
// So we're forced to always set this to 1; regardless if the answer came from the cache or not.
// On newer systems(e.g. ubuntu 16.04 with glib version 2.23), this issue is resolved.
// So we may set this bit back to 0 in the future ?
func (i *item) toMsg(m *dns.Msg, now time.Time, do bool, ad bool) *dns.Msg {
	m1 := new(dns.Msg)
	m1.SetReply(m)

	// Set this to true as some DNS clients discard the *entire* packet when it's non-authoritative.
	// This is probably not according to spec, but the bit itself is not super useful as this point, so
	// just set it to true.
	m1.Authoritative = true
	m1.AuthenticatedData = i.AuthenticatedData
	if !do && !ad {
		// When DNSSEC was not wanted, it can't be authenticated data.
		// However, retain the AD bit if the requester set the AD bit, per RFC6840 5.7-5.8
		m1.AuthenticatedData = false
	}
	m1.RecursionAvailable = i.RecursionAvailable
	m1.Rcode = i.Rcode

	m1.Answer = make([]dns.RR, len(i.Answer))
	m1.Ns = make([]dns.RR, len(i.Ns))
	m1.Extra = make([]dns.RR, len(i.Extra))

	ttl := uint32(i.ttl(now))
	m1.Answer = filterRRSlice(i.Answer, ttl, true)
	m1.Ns = filterRRSlice(i.Ns, ttl, true)
	m1.Extra = filterRRSlice(i.Extra, ttl, true)

	return m1
}

func (i *item) ttl(now time.Time) int {
	ttl := int(i.origTTL) - int(now.UTC().Sub(i.stored).Seconds())
	return ttl
}

func (i *item) matches(state request.Request) bool {
	if state.QType() == i.QType && strings.EqualFold(state.QName(), i.Name) {
		return true
	}
	return false
}
