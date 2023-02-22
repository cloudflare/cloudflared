package vars

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Request* and Response* are the prometheus counters and gauges we are using for exporting metrics.
var (
	RequestCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: subsystem,
		Name:      "requests_total",
		Help:      "Counter of DNS requests made per zone, protocol and family.",
	}, []string{"server", "zone", "view", "proto", "family", "type"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: plugin.Namespace,
		Subsystem: subsystem,
		Name:      "request_duration_seconds",
		Buckets:   plugin.TimeBuckets,
		Help:      "Histogram of the time (in seconds) each request took per zone.",
	}, []string{"server", "zone", "view"})

	RequestSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: plugin.Namespace,
		Subsystem: subsystem,
		Name:      "request_size_bytes",
		Help:      "Size of the EDNS0 UDP buffer in bytes (64K for TCP) per zone and protocol.",
		Buckets:   []float64{0, 100, 200, 300, 400, 511, 1023, 2047, 4095, 8291, 16e3, 32e3, 48e3, 64e3},
	}, []string{"server", "zone", "view", "proto"})

	RequestDo = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: subsystem,
		Name:      "do_requests_total",
		Help:      "Counter of DNS requests with DO bit set per zone.",
	}, []string{"server", "zone", "view"})

	ResponseSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: plugin.Namespace,
		Subsystem: subsystem,
		Name:      "response_size_bytes",
		Help:      "Size of the returned response in bytes.",
		Buckets:   []float64{0, 100, 200, 300, 400, 511, 1023, 2047, 4095, 8291, 16e3, 32e3, 48e3, 64e3},
	}, []string{"server", "zone", "view", "proto"})

	ResponseRcode = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: subsystem,
		Name:      "responses_total",
		Help:      "Counter of response status codes.",
	}, []string{"server", "zone", "view", "rcode", "plugin"})

	Panic = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Name:      "panics_total",
		Help:      "A metrics that counts the number of panics.",
	})

	PluginEnabled = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: plugin.Namespace,
		Name:      "plugin_enabled",
		Help:      "A metric that indicates whether a plugin is enabled on per server and zone basis.",
	}, []string{"server", "zone", "view", "name"})

	HTTPSResponsesCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: subsystem,
		Name:      "https_responses_total",
		Help:      "Counter of DoH responses per server and http status code.",
	}, []string{"server", "status"})
)

const (
	subsystem = "dns"

	// Dropped indicates we dropped the query before any handling. It has no closing dot, so it can not be a valid zone.
	Dropped = "dropped"
)
