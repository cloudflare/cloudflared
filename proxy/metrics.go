package proxy

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudflare/cloudflared/connection"
)

// Metrics uses connection.MetricsNamespace(aka cloudflared) as namespace and connection.TunnelSubsystem
// (tunnel) as subsystem to keep them consistent with the previous qualifier.

var (
	totalRequests = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: connection.MetricsNamespace,
			Subsystem: connection.TunnelSubsystem,
			Name:      "total_requests",
			Help:      "Amount of requests proxied through all the tunnels",
		},
	)
	concurrentRequests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: connection.MetricsNamespace,
			Subsystem: connection.TunnelSubsystem,
			Name:      "concurrent_requests_per_tunnel",
			Help:      "Concurrent requests proxied through each tunnel",
		},
	)
	responseByCode = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: connection.MetricsNamespace,
			Subsystem: connection.TunnelSubsystem,
			Name:      "response_by_code",
			Help:      "Count of responses by HTTP status code",
		},
		[]string{"status_code"},
	)
	requestErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: connection.MetricsNamespace,
			Subsystem: connection.TunnelSubsystem,
			Name:      "request_errors",
			Help:      "Count of error proxying to origin",
		},
	)
	activeTCPSessions = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: connection.MetricsNamespace,
			Subsystem: "tcp",
			Name:      "active_sessions",
			Help:      "Concurrent count of TCP sessions that are being proxied to any origin",
		},
	)
	totalTCPSessions = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: connection.MetricsNamespace,
			Subsystem: "tcp",
			Name:      "total_sessions",
			Help:      "Total count of TCP sessions that have been proxied to any origin",
		},
	)
)

func init() {
	prometheus.MustRegister(
		totalRequests,
		concurrentRequests,
		responseByCode,
		requestErrors,
		activeTCPSessions,
		totalTCPSessions,
	)
}

func incrementRequests() {
	totalRequests.Inc()
	concurrentRequests.Inc()
}

func decrementConcurrentRequests() {
	concurrentRequests.Dec()
}

func incrementTCPRequests() {
	incrementRequests()
	totalTCPSessions.Inc()
	activeTCPSessions.Inc()
}

func decrementTCPConcurrentRequests() {
	decrementConcurrentRequests()
	activeTCPSessions.Dec()
}
