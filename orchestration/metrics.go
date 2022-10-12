package orchestration

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	MetricsNamespace = "cloudflared"
	MetricsSubsystem = "orchestration"
)

var (
	configVersion = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "config_version",
			Help:      "Configuration Version",
		},
	)
)

func init() {
	prometheus.MustRegister(configVersion)
}
