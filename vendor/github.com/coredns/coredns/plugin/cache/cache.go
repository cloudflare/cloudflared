// Package cache implements a cache.
package cache

import (
	"hash/fnv"
	"net"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/cache"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/coredns/coredns/plugin/pkg/response"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// Cache is a plugin that looks up responses in a cache and caches replies.
// It has a success and a denial of existence cache.
type Cache struct {
	Next  plugin.Handler
	Zones []string

	zonesMetricLabel string
	viewMetricLabel  string

	ncache  *cache.Cache
	ncap    int
	nttl    time.Duration
	minnttl time.Duration

	pcache  *cache.Cache
	pcap    int
	pttl    time.Duration
	minpttl time.Duration
	failttl time.Duration // TTL for caching SERVFAIL responses

	// Prefetch.
	prefetch   int
	duration   time.Duration
	percentage int

	// Stale serve
	staleUpTo   time.Duration
	verifyStale bool

	// Positive/negative zone exceptions
	pexcept []string
	nexcept []string

	// Keep ttl option
	keepttl bool

	// Testing.
	now func() time.Time
}

// New returns an initialized Cache with default settings. It's up to the
// caller to set the Next handler.
func New() *Cache {
	return &Cache{
		Zones:      []string{"."},
		pcap:       defaultCap,
		pcache:     cache.New(defaultCap),
		pttl:       maxTTL,
		minpttl:    minTTL,
		ncap:       defaultCap,
		ncache:     cache.New(defaultCap),
		nttl:       maxNTTL,
		minnttl:    minNTTL,
		failttl:    minNTTL,
		prefetch:   0,
		duration:   1 * time.Minute,
		percentage: 10,
		now:        time.Now,
	}
}

// key returns key under which we store the item, -1 will be returned if we don't store the message.
// Currently we do not cache Truncated, errors zone transfers or dynamic update messages.
// qname holds the already lowercased qname.
func key(qname string, m *dns.Msg, t response.Type, do, cd bool) (bool, uint64) {
	// We don't store truncated responses.
	if m.Truncated {
		return false, 0
	}
	// Nor errors or Meta or Update.
	if t == response.OtherError || t == response.Meta || t == response.Update {
		return false, 0
	}

	return true, hash(qname, m.Question[0].Qtype, do, cd)
}

var one = []byte("1")
var zero = []byte("0")

func hash(qname string, qtype uint16, do, cd bool) uint64 {
	h := fnv.New64()

	if do {
		h.Write(one)
	} else {
		h.Write(zero)
	}

	if cd {
		h.Write(one)
	} else {
		h.Write(zero)
	}

	h.Write([]byte{byte(qtype >> 8)})
	h.Write([]byte{byte(qtype)})
	h.Write([]byte(qname))
	return h.Sum64()
}

func computeTTL(msgTTL, minTTL, maxTTL time.Duration) time.Duration {
	ttl := msgTTL
	if ttl < minTTL {
		ttl = minTTL
	}
	if ttl > maxTTL {
		ttl = maxTTL
	}
	return ttl
}

// ResponseWriter is a response writer that caches the reply message.
type ResponseWriter struct {
	dns.ResponseWriter
	*Cache
	state  request.Request
	server string // Server handling the request.

	do         bool // When true the original request had the DO bit set.
	cd         bool // When true the original request had the CD bit set.
	ad         bool // When true the original request had the AD bit set.
	prefetch   bool // When true write nothing back to the client.
	remoteAddr net.Addr

	wildcardFunc func() string // function to retrieve wildcard name that synthesized the result.

	pexcept []string // positive zone exceptions
	nexcept []string // negative zone exceptions
}

// newPrefetchResponseWriter returns a Cache ResponseWriter to be used in
// prefetch requests. It ensures RemoteAddr() can be called even after the
// original connection has already been closed.
func newPrefetchResponseWriter(server string, state request.Request, c *Cache) *ResponseWriter {
	// Resolve the address now, the connection might be already closed when the
	// actual prefetch request is made.
	addr := state.W.RemoteAddr()
	// The protocol of the client triggering a cache prefetch doesn't matter.
	// The address type is used by request.Proto to determine the response size,
	// and using TCP ensures the message isn't unnecessarily truncated.
	if u, ok := addr.(*net.UDPAddr); ok {
		addr = &net.TCPAddr{IP: u.IP, Port: u.Port, Zone: u.Zone}
	}

	return &ResponseWriter{
		ResponseWriter: state.W,
		Cache:          c,
		state:          state,
		server:         server,
		do:             state.Do(),
		cd:             state.Req.CheckingDisabled,
		prefetch:       true,
		remoteAddr:     addr,
	}
}

// RemoteAddr implements the dns.ResponseWriter interface.
func (w *ResponseWriter) RemoteAddr() net.Addr {
	if w.remoteAddr != nil {
		return w.remoteAddr
	}
	return w.ResponseWriter.RemoteAddr()
}

// WriteMsg implements the dns.ResponseWriter interface.
func (w *ResponseWriter) WriteMsg(res *dns.Msg) error {
	mt, _ := response.Typify(res, w.now().UTC())

	// key returns empty string for anything we don't want to cache.
	hasKey, key := key(w.state.Name(), res, mt, w.do, w.cd)

	msgTTL := dnsutil.MinimalTTL(res, mt)
	var duration time.Duration
	switch mt {
	case response.NameError, response.NoData:
		duration = computeTTL(msgTTL, w.minnttl, w.nttl)
	case response.ServerError:
		duration = w.failttl
	default:
		duration = computeTTL(msgTTL, w.minpttl, w.pttl)
	}

	if hasKey && duration > 0 {
		if w.state.Match(res) {
			w.set(res, key, mt, duration)
			cacheSize.WithLabelValues(w.server, Success, w.zonesMetricLabel, w.viewMetricLabel).Set(float64(w.pcache.Len()))
			cacheSize.WithLabelValues(w.server, Denial, w.zonesMetricLabel, w.viewMetricLabel).Set(float64(w.ncache.Len()))
		} else {
			// Don't log it, but increment counter
			cacheDrops.WithLabelValues(w.server, w.zonesMetricLabel, w.viewMetricLabel).Inc()
		}
	}

	if w.prefetch {
		return nil
	}

	// Apply capped TTL to this reply to avoid jarring TTL experience 1799 -> 8 (e.g.)
	ttl := uint32(duration.Seconds())
	res.Answer = filterRRSlice(res.Answer, ttl, false)
	res.Ns = filterRRSlice(res.Ns, ttl, false)
	res.Extra = filterRRSlice(res.Extra, ttl, false)

	if !w.do && !w.ad {
		// unset AD bit if requester is not OK with DNSSEC
		// But retain AD bit if requester set the AD bit in the request, per RFC6840 5.7-5.8
		res.AuthenticatedData = false
	}

	return w.ResponseWriter.WriteMsg(res)
}

func (w *ResponseWriter) set(m *dns.Msg, key uint64, mt response.Type, duration time.Duration) {
	// duration is expected > 0
	// and key is valid
	switch mt {
	case response.NoError, response.Delegation:
		if plugin.Zones(w.pexcept).Matches(m.Question[0].Name) != "" {
			// zone is in exception list, do not cache
			return
		}
		i := newItem(m, w.now(), duration)
		if w.wildcardFunc != nil {
			i.wildcard = w.wildcardFunc()
		}
		if w.pcache.Add(key, i) {
			evictions.WithLabelValues(w.server, Success, w.zonesMetricLabel, w.viewMetricLabel).Inc()
		}
		// when pre-fetching, remove the negative cache entry if it exists
		if w.prefetch {
			w.ncache.Remove(key)
		}

	case response.NameError, response.NoData, response.ServerError:
		if plugin.Zones(w.nexcept).Matches(m.Question[0].Name) != "" {
			// zone is in exception list, do not cache
			return
		}
		i := newItem(m, w.now(), duration)
		if w.wildcardFunc != nil {
			i.wildcard = w.wildcardFunc()
		}
		if w.ncache.Add(key, i) {
			evictions.WithLabelValues(w.server, Denial, w.zonesMetricLabel, w.viewMetricLabel).Inc()
		}

	case response.OtherError:
		// don't cache these
	default:
		log.Warningf("Caching called with unknown classification: %d", mt)
	}
}

// Write implements the dns.ResponseWriter interface.
func (w *ResponseWriter) Write(buf []byte) (int, error) {
	log.Warning("Caching called with Write: not caching reply")
	if w.prefetch {
		return 0, nil
	}
	n, err := w.ResponseWriter.Write(buf)
	return n, err
}

// verifyStaleResponseWriter is a response writer that only writes messages if they should replace a
// stale cache entry, and otherwise discards them.
type verifyStaleResponseWriter struct {
	*ResponseWriter
	refreshed bool // set to true if the last WriteMsg wrote to ResponseWriter, false otherwise.
}

// newVerifyStaleResponseWriter returns a ResponseWriter to be used when verifying stale cache
// entries. It only forward writes if an entry was successfully refreshed according to RFC8767,
// section 4 (response is NoError or NXDomain), and ignores any other response.
func newVerifyStaleResponseWriter(w *ResponseWriter) *verifyStaleResponseWriter {
	return &verifyStaleResponseWriter{
		w,
		false,
	}
}

// WriteMsg implements the dns.ResponseWriter interface.
func (w *verifyStaleResponseWriter) WriteMsg(res *dns.Msg) error {
	w.refreshed = false
	if res.Rcode == dns.RcodeSuccess || res.Rcode == dns.RcodeNameError {
		w.refreshed = true
		return w.ResponseWriter.WriteMsg(res) // stores to the cache and send to client
	}
	return nil // else discard
}

const (
	maxTTL  = dnsutil.MaximumDefaulTTL
	minTTL  = dnsutil.MinimalDefaultTTL
	maxNTTL = dnsutil.MaximumDefaulTTL / 2
	minNTTL = dnsutil.MinimalDefaultTTL

	defaultCap = 10000 // default capacity of the cache.

	// Success is the class for caching positive caching.
	Success = "success"
	// Denial is the class defined for negative caching.
	Denial = "denial"
)
