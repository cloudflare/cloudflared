# cache

## Name

*cache* - enables a frontend cache.

## Description

With *cache* enabled, all records except zone transfers and metadata records will be cached for up to
3600s. Caching is mostly useful in a scenario when fetching data from the backend (upstream,
database, etc.) is expensive.

*Cache* will pass DNSSEC (DNSSEC OK; DO) options through the plugin for upstream queries.

This plugin can only be used once per Server Block.

## Syntax

~~~ txt
cache [TTL] [ZONES...]
~~~

* **TTL** max TTL in seconds. If not specified, the maximum TTL will be used, which is 3600 for
    NOERROR responses and 1800 for denial of existence ones.
    Setting a TTL of 300: `cache 300` would cache records up to 300 seconds.
* **ZONES** zones it should cache for. If empty, the zones from the configuration block are used.

Each element in the cache is cached according to its TTL (with **TTL** as the max).
A cache is divided into 256 shards, each holding up to 39 items by default - for a total size
of 256 * 39 = 9984 items.

If you want more control:

~~~ txt
cache [TTL] [ZONES...] {
    success CAPACITY [TTL] [MINTTL]
    denial CAPACITY [TTL] [MINTTL]
    prefetch AMOUNT [[DURATION] [PERCENTAGE%]]
    serve_stale [DURATION] [REFRESH_MODE]
    servfail DURATION
    disable success|denial [ZONES...]
    keepttl
}
~~~

* **TTL**  and **ZONES** as above.
* `success`, override the settings for caching successful responses. **CAPACITY** indicates the maximum
  number of packets we cache before we start evicting (*randomly*). **TTL** overrides the cache maximum TTL.
  **MINTTL** overrides the cache minimum TTL (default 5), which can be useful to limit queries to the backend.
* `denial`, override the settings for caching denial of existence responses. **CAPACITY** indicates the maximum
  number of packets we cache before we start evicting (LRU). **TTL** overrides the cache maximum TTL.
  **MINTTL** overrides the cache minimum TTL (default 5), which can be useful to limit queries to the backend.
  There is a third category (`error`) but those responses are never cached.
* `prefetch` will prefetch popular items when they are about to be expunged from the cache.
  Popular means **AMOUNT** queries have been seen with no gaps of **DURATION** or more between them.
  **DURATION** defaults to 1m. Prefetching will happen when the TTL drops below **PERCENTAGE**,
  which defaults to `10%`, or latest 1 second before TTL expiration. Values should be in the range `[10%, 90%]`.
  Note the percent sign is mandatory. **PERCENTAGE** is treated as an `int`.
* `serve_stale`, when serve\_stale is set, cache will always serve an expired entry to a client if there is one
  available as long as it has not been expired for longer than **DURATION** (default 1 hour). By default, the _cache_ plugin will
  attempt to refresh the cache entry after sending the expired cache entry to the client. The
  responses have a TTL of 0. **REFRESH_MODE** controls the timing of the expired cache entry refresh.
  `verify` will first verify that an entry is still unavailable from the source before sending the expired entry to the client.
  `immediate` will immediately send the expired entry to the client before
  checking to see if the entry is available from the source. **REFRESH_MODE** defaults to `immediate`. Setting this
  value to `verify` can lead to increased latency when serving stale responses, but will prevent stale entries
  from ever being served if an updated response can be retrieved from the source.
* `servfail` cache SERVFAIL responses for **DURATION**.  Setting **DURATION** to 0 will disable caching of SERVFAIL
  responses.  If this option is not set, SERVFAIL responses will be cached for 5 seconds.  **DURATION** may not be
  greater than 5 minutes.
* `disable`  disable the success or denial cache for the listed **ZONES**.  If no **ZONES** are given, the specified
  cache will be disabled for all zones.
* `keepttl` do not age TTL when serving responses from cache. The entry will still be removed from cache
  when the TTL expires as normal, but until it expires responses will include the original TTL instead
  of the remaining TTL. This can be useful if CoreDNS is used as an authoritative server and you want
  to serve a consistent TTL to downstream clients. This is **NOT** recommended when CoreDNS is caching
  records it is not authoritative for because it could result in downstream clients using stale answers.

## Capacity and Eviction

If **CAPACITY** _is not_ specified, the default cache size is 9984 per cache. The minimum allowed cache size is 1024.
If **CAPACITY** _is_ specified, the actual cache size used will be rounded down to the nearest number divisible by 256 (so all shards are equal in size).

Eviction is done per shard. In effect, when a shard reaches capacity, items are evicted from that shard.
Since shards don't fill up perfectly evenly, evictions will occur before the entire cache reaches full capacity.
Each shard capacity is equal to the total cache size / number of shards (256). Eviction is random, not TTL based.
Entries with 0 TTL will remain in the cache until randomly evicted when the shard reaches capacity.

## Metrics

If monitoring is enabled (via the *prometheus* plugin) then the following metrics are exported:

* `coredns_cache_entries{server, type, zones, view}` - Total elements in the cache by cache type.
* `coredns_cache_hits_total{server, type, zones, view}` - Counter of cache hits by cache type.
* `coredns_cache_misses_total{server, zones, view}` - Counter of cache misses. - Deprecated, derive misses from cache hits/requests counters.
* `coredns_cache_requests_total{server, zones, view}` - Counter of cache requests.
* `coredns_cache_prefetch_total{server, zones, view}` - Counter of times the cache has prefetched a cached item.
* `coredns_cache_drops_total{server, zones, view}` - Counter of responses excluded from the cache due to request/response question name mismatch.
* `coredns_cache_served_stale_total{server, zones, view}` - Counter of requests served from stale cache entries.
* `coredns_cache_evictions_total{server, type, zones, view}` - Counter of cache evictions.

Cache types are either "denial" or "success". `Server` is the server handling the request, see the
prometheus plugin for documentation.

## Examples

Enable caching for all zones, but cap everything to a TTL of 10 seconds:

~~~ corefile
. {
    cache 10
    whoami
}
~~~

Proxy to Google Public DNS and only cache responses for example.org (or below).

~~~ corefile
. {
    forward . 8.8.8.8:53
    cache example.org
}
~~~

Enable caching for `example.org`, keep a positive cache size of 5000 and a negative cache size of 2500:

~~~ corefile
example.org {
    cache {
        success 5000
        denial 2500
    }
}
~~~

Enable caching for `example.org`, but do not cache denials in `sub.example.org`:

~~~ corefile
example.org {
    cache {
        disable denial sub.example.org
    }
}
~~~
