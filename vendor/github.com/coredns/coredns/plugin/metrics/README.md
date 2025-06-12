# prometheus

## Name

*prometheus* - enables [Prometheus](https://prometheus.io/) metrics.

## Description

With *prometheus* you export metrics from CoreDNS and any plugin that has them.
The default location for the metrics is `localhost:9153`. The metrics path is fixed to `/metrics`.

In addition to the default Go metrics exported by the [Prometheus Go client](https://prometheus.io/docs/guides/go-application/),
the following metrics are exported:

* `coredns_build_info{version, revision, goversion}` - info about CoreDNS itself.
* `coredns_panics_total{}` - total number of panics.
* `coredns_dns_requests_total{server, zone, view, proto, family, type}` - total query count.
* `coredns_dns_request_duration_seconds{server, zone, view, type}` - duration to process each query.
* `coredns_dns_request_size_bytes{server, zone, view, proto}` - size of the request in bytes. Uses the original size before any plugin rewrites.
* `coredns_dns_do_requests_total{server, view, zone}` -  queries that have the DO bit set
* `coredns_dns_response_size_bytes{server, zone, view, proto}` - response size in bytes.
* `coredns_dns_responses_total{server, zone, view, rcode, plugin}` - response per zone, rcode and plugin.
* `coredns_dns_https_responses_total{server, status}` - responses per server and http status code.
* `coredns_dns_quic_responses_total{server, status}` - responses per server and QUIC application code.
* `coredns_plugin_enabled{server, zone, view, name}` - indicates whether a plugin is enabled on per server, zone and view basis.

Almost each counter has a label `zone` which is the zonename used for the request/response.

Extra labels used are:

* `server` is identifying the server responsible for the request. This is a string formatted
  as the server's listening address: `<scheme>://[<bind>]:<port>`. I.e. for a "normal" DNS server
  this is `dns://:53`. If you are using the *bind* plugin an IP address is included, e.g.: `dns://127.0.0.53:53`.
* `proto` which holds the transport of the response ("udp" or "tcp")
* The address family (`family`) of the transport (1 = IP (IP version 4), 2 = IP6 (IP version 6)).
* `type` which holds the query type. It holds most common types (A, AAAA, MX, SOA, CNAME, PTR, TXT,
  NS, SRV, DS, DNSKEY, RRSIG, NSEC, NSEC3, HTTPS, IXFR, AXFR and ANY) and "other" which lumps together all
  other types.
* `status` which holds the https status code. Possible values are:
  * 200 - request is processed,
  * 404 - request has been rejected on validation,
  * 400 - request to dns message conversion failed,
  * 500 - processing ended up with no response.
* the `plugin` label holds the name of the plugin that made the write to the client. If the server
  did the write (on error for instance), the value is empty.

If monitoring is enabled, queries that do not enter the plugin chain are exported under the fake
name "dropped" (without a closing dot - this is never a valid domain name).

Other plugins may export additional stats when the _prometheus_ plugin is enabled.  Those stats are documented in each
plugin's README.

This plugin can only be used once per Server Block.

## Syntax

~~~
prometheus [ADDRESS]
~~~

For each zone that you want to see metrics for.

It optionally takes a bind address to which the metrics are exported; the default
listens on `localhost:9153`. The metrics path is fixed to `/metrics`.

## Examples

Use an alternative listening address:

~~~ corefile
. {
    prometheus localhost:9253
}
~~~

Or via an environment variable (this is supported throughout the Corefile): `export PORT=9253`, and
then:

~~~ corefile
. {
    prometheus localhost:{$PORT}
}
~~~

## Bugs

When reloading, the Prometheus handler is stopped before the new server instance is started.
If that new server fails to start, then the initial server instance is still available and DNS queries still served,
but Prometheus handler stays down.
Prometheus will not reply HTTP request until a successful reload or a complete restart of CoreDNS.
Only the plugins that register as Handler are visible in `coredns_plugin_enabled{server, zone, name}`. As of today the plugins reload and bind will not be reported.
