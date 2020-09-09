package cache

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// cacheSize is total elements in the cache by cache type.
	cacheSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "entries",
		Help:      "The number of elements in the cache.",
	}, []string{"server", "type"})
	// cacheHits is counter of cache hits by cache type.
	cacheHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "hits_total",
		Help:      "The count of cache hits.",
	}, []string{"server", "type"})
	// cacheMisses is the counter of cache misses.
	cacheMisses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "misses_total",
		Help:      "The count of cache misses.",
	}, []string{"server"})
	// cachePrefetches is the number of time the cache has prefetched a cached item.
	cachePrefetches = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "prefetch_total",
		Help:      "The number of time the cache has prefetched a cached item.",
	}, []string{"server"})
	// cacheDrops is the number responses that are not cached, because the reply is malformed.
	cacheDrops = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "drops_total",
		Help:      "The number responses that are not cached, because the reply is malformed.",
	}, []string{"server"})
	// servedStale is the number of requests served from stale cache entries.
	servedStale = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "cache",
		Name:      "served_stale_total",
		Help:      "The number of requests served from stale cache entries.",
	}, []string{"server"})
)
