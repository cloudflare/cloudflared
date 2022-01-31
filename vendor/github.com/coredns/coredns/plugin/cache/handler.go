package cache

import (
	"context"
	"math"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	"github.com/coredns/coredns/request"

	"github.com/miekg/dns"
)

// ServeDNS implements the plugin.Handler interface.
func (c *Cache) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	rc := r.Copy() // We potentially modify r, to prevent other plugins from seeing this (r is a pointer), copy r into rc.
	state := request.Request{W: w, Req: rc}
	do := state.Do()

	zone := plugin.Zones(c.Zones).Matches(state.Name())
	if zone == "" {
		return plugin.NextOrFailure(c.Name(), c.Next, ctx, w, rc)
	}

	now := c.now().UTC()
	server := metrics.WithServer(ctx)

	// On cache miss, if the request has the OPT record and the DO bit set we leave the message as-is. If there isn't a DO bit
	// set we will modify the request to _add_ one. This means we will always do DNSSEC lookups on cache misses.
	// When writing to cache, any DNSSEC RRs in the response are written to cache with the response.
	// When sending a response to a non-DNSSEC client, we remove DNSSEC RRs from the response. We use a 2048 buffer size, which is
	// less than 4096 (and older default) and more than 1024 which may be too small. We might need to tweaks this
	// value to be smaller still to prevent UDP fragmentation?

	ttl := 0
	i := c.getIgnoreTTL(now, state, server)
	if i != nil {
		ttl = i.ttl(now)
	}
	if i == nil {
		crr := &ResponseWriter{ResponseWriter: w, Cache: c, state: state, server: server, do: do}
		return c.doRefresh(ctx, state, crr)
	}
	if ttl < 0 {
		servedStale.WithLabelValues(server).Inc()
		// Adjust the time to get a 0 TTL in the reply built from a stale item.
		now = now.Add(time.Duration(ttl) * time.Second)
		cw := newPrefetchResponseWriter(server, state, c)
		go c.doPrefetch(ctx, state, cw, i, now)
	} else if c.shouldPrefetch(i, now) {
		cw := newPrefetchResponseWriter(server, state, c)
		go c.doPrefetch(ctx, state, cw, i, now)
	}
	resp := i.toMsg(r, now, do)
	w.WriteMsg(resp)

	return dns.RcodeSuccess, nil
}

func (c *Cache) doPrefetch(ctx context.Context, state request.Request, cw *ResponseWriter, i *item, now time.Time) {
	cachePrefetches.WithLabelValues(cw.server).Inc()
	c.doRefresh(ctx, state, cw)

	// When prefetching we loose the item i, and with it the frequency
	// that we've gathered sofar. See we copy the frequencies info back
	// into the new item that was stored in the cache.
	if i1 := c.exists(state); i1 != nil {
		i1.Freq.Reset(now, i.Freq.Hits())
	}
}

func (c *Cache) doRefresh(ctx context.Context, state request.Request, cw *ResponseWriter) (int, error) {
	if !state.Do() {
		setDo(state.Req)
	}
	return plugin.NextOrFailure(c.Name(), c.Next, ctx, cw, state.Req)
}

func (c *Cache) shouldPrefetch(i *item, now time.Time) bool {
	if c.prefetch <= 0 {
		return false
	}
	i.Freq.Update(c.duration, now)
	threshold := int(math.Ceil(float64(c.percentage) / 100 * float64(i.origTTL)))
	return i.Freq.Hits() >= c.prefetch && i.ttl(now) <= threshold
}

// Name implements the Handler interface.
func (c *Cache) Name() string { return "cache" }

func (c *Cache) get(now time.Time, state request.Request, server string) (*item, bool) {
	k := hash(state.Name(), state.QType())
	cacheRequests.WithLabelValues(server).Inc()

	if i, ok := c.ncache.Get(k); ok && i.(*item).ttl(now) > 0 {
		cacheHits.WithLabelValues(server, Denial).Inc()
		return i.(*item), true
	}

	if i, ok := c.pcache.Get(k); ok && i.(*item).ttl(now) > 0 {
		cacheHits.WithLabelValues(server, Success).Inc()
		return i.(*item), true
	}
	cacheMisses.WithLabelValues(server).Inc()
	return nil, false
}

// getIgnoreTTL unconditionally returns an item if it exists in the cache.
func (c *Cache) getIgnoreTTL(now time.Time, state request.Request, server string) *item {
	k := hash(state.Name(), state.QType())
	cacheRequests.WithLabelValues(server).Inc()

	if i, ok := c.ncache.Get(k); ok {
		ttl := i.(*item).ttl(now)
		if ttl > 0 || (c.staleUpTo > 0 && -ttl < int(c.staleUpTo.Seconds())) {
			cacheHits.WithLabelValues(server, Denial).Inc()
			return i.(*item)
		}
	}
	if i, ok := c.pcache.Get(k); ok {
		ttl := i.(*item).ttl(now)
		if ttl > 0 || (c.staleUpTo > 0 && -ttl < int(c.staleUpTo.Seconds())) {
			cacheHits.WithLabelValues(server, Success).Inc()
			return i.(*item)
		}
	}
	cacheMisses.WithLabelValues(server).Inc()
	return nil
}

func (c *Cache) exists(state request.Request) *item {
	k := hash(state.Name(), state.QType())
	if i, ok := c.ncache.Get(k); ok {
		return i.(*item)
	}
	if i, ok := c.pcache.Get(k); ok {
		return i.(*item)
	}
	return nil
}

// setDo sets the DO bit and UDP buffer size in the message m.
func setDo(m *dns.Msg) {
	o := m.IsEdns0()
	if o != nil {
		o.SetDo()
		o.SetUDPSize(defaultUDPBufSize)
		return
	}

	o = &dns.OPT{Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeOPT}}
	o.SetDo()
	o.SetUDPSize(defaultUDPBufSize)
	m.Extra = append(m.Extra, o)
}

// defaultUDPBufsize is the bufsize the cache plugin uses on outgoing requests that don't
// have an OPT RR.
const defaultUDPBufSize = 2048
