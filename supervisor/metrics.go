package supervisor

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudflare/cloudflared/connection"
)

// Metrics uses connection.MetricsNamespace(aka cloudflared) as namespace and connection.TunnelSubsystem
// (tunnel) as subsystem to keep them consistent with the previous qualifier.

var (
	haConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: connection.MetricsNamespace,
			Subsystem: connection.TunnelSubsystem,
			Name:      "ha_connections",
			Help:      "Number of active ha connections",
		},
	)
)

func init() {
	prometheus.MustRegister(
		haConnections,
	)
}
