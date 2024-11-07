package v3

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "cloudflared"
	subsystem = "udp"
)

type Metrics interface {
	IncrementFlows()
	DecrementFlows()
	PayloadTooLarge()
	RetryFlowResponse()
	MigrateFlow()
}

type metrics struct {
	activeUDPFlows     prometheus.Gauge
	totalUDPFlows      prometheus.Counter
	payloadTooLarge    prometheus.Counter
	retryFlowResponses prometheus.Counter
	migratedFlows      prometheus.Counter
}

func (m *metrics) IncrementFlows() {
	m.totalUDPFlows.Inc()
	m.activeUDPFlows.Inc()
}

func (m *metrics) DecrementFlows() {
	m.activeUDPFlows.Dec()
}

func (m *metrics) PayloadTooLarge() {
	m.payloadTooLarge.Inc()
}

func (m *metrics) RetryFlowResponse() {
	m.retryFlowResponses.Inc()
}

func (m *metrics) MigrateFlow() {
	m.migratedFlows.Inc()
}

func NewMetrics(registerer prometheus.Registerer) Metrics {
	m := &metrics{
		activeUDPFlows: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "active_flows",
			Help:      "Concurrent count of UDP flows that are being proxied to any origin",
		}),
		totalUDPFlows: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "total_flows",
			Help:      "Total count of UDP flows that have been proxied to any origin",
		}),
		payloadTooLarge: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "payload_too_large",
			Help:      "Total count of UDP flows that have had origin payloads that are too large to proxy",
		}),
		retryFlowResponses: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "retry_flow_responses",
			Help:      "Total count of UDP flows that have had to send their registration response more than once",
		}),
		migratedFlows: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "migrated_flows",
			Help:      "Total count of UDP flows have been migrated across local connections",
		}),
	}
	registerer.MustRegister(
		m.activeUDPFlows,
		m.totalUDPFlows,
		m.payloadTooLarge,
		m.retryFlowResponses,
		m.migratedFlows,
	)
	return m
}
