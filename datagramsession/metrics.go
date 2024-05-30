package datagramsession

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "cloudflared"
)

var (
	activeUDPSessions = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "udp",
		Name:      "active_sessions",
		Help:      "Concurrent count of UDP sessions that are being proxied to any origin",
	})
	totalUDPSessions = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "udp",
		Name:      "total_sessions",
		Help:      "Total count of UDP sessions that have been proxied to any origin",
	})
)

func init() {
	prometheus.MustRegister(
		activeUDPSessions,
		totalUDPSessions,
	)
}

func incrementUDPSessions() {
	totalUDPSessions.Inc()
	activeUDPSessions.Inc()
}

func decrementUDPActiveSessions() {
	activeUDPSessions.Dec()
}
