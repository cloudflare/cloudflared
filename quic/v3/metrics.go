package v3

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudflare/cloudflared/quic"
)

const (
	namespace = "cloudflared"
	subsystem = "udp"

	commandMetricLabel = "command"
)

type Metrics interface {
	IncrementFlows(connIndex uint8)
	DecrementFlows(connIndex uint8)
	PayloadTooLarge(connIndex uint8)
	RetryFlowResponse(connIndex uint8)
	MigrateFlow(connIndex uint8)
	UnsupportedRemoteCommand(connIndex uint8, command string)
}

type metrics struct {
	activeUDPFlows            *prometheus.GaugeVec
	totalUDPFlows             *prometheus.CounterVec
	payloadTooLarge           *prometheus.CounterVec
	retryFlowResponses        *prometheus.CounterVec
	migratedFlows             *prometheus.CounterVec
	unsupportedRemoteCommands *prometheus.CounterVec
}

func (m *metrics) IncrementFlows(connIndex uint8) {
	m.totalUDPFlows.WithLabelValues(fmt.Sprintf("%d", connIndex)).Inc()
	m.activeUDPFlows.WithLabelValues(fmt.Sprintf("%d", connIndex)).Inc()
}

func (m *metrics) DecrementFlows(connIndex uint8) {
	m.activeUDPFlows.WithLabelValues(fmt.Sprintf("%d", connIndex)).Dec()
}

func (m *metrics) PayloadTooLarge(connIndex uint8) {
	m.payloadTooLarge.WithLabelValues(fmt.Sprintf("%d", connIndex)).Inc()
}

func (m *metrics) RetryFlowResponse(connIndex uint8) {
	m.retryFlowResponses.WithLabelValues(fmt.Sprintf("%d", connIndex)).Inc()
}

func (m *metrics) MigrateFlow(connIndex uint8) {
	m.migratedFlows.WithLabelValues(fmt.Sprintf("%d", connIndex)).Inc()
}

func (m *metrics) UnsupportedRemoteCommand(connIndex uint8, command string) {
	m.unsupportedRemoteCommands.WithLabelValues(fmt.Sprintf("%d", connIndex), command).Inc()
}

func NewMetrics(registerer prometheus.Registerer) Metrics {
	m := &metrics{
		activeUDPFlows: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "active_flows",
			Help:      "Concurrent count of UDP flows that are being proxied to any origin",
		}, []string{quic.ConnectionIndexMetricLabel}),
		totalUDPFlows: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "total_flows",
			Help:      "Total count of UDP flows that have been proxied to any origin",
		}, []string{quic.ConnectionIndexMetricLabel}),
		payloadTooLarge: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "payload_too_large",
			Help:      "Total count of UDP flows that have had origin payloads that are too large to proxy",
		}, []string{quic.ConnectionIndexMetricLabel}),
		retryFlowResponses: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "retry_flow_responses",
			Help:      "Total count of UDP flows that have had to send their registration response more than once",
		}, []string{quic.ConnectionIndexMetricLabel}),
		migratedFlows: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "migrated_flows",
			Help:      "Total count of UDP flows have been migrated across local connections",
		}, []string{quic.ConnectionIndexMetricLabel}),
		unsupportedRemoteCommands: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "unsupported_remote_command_total",
			Help:      "Total count of unsupported remote RPC commands for the ",
		}, []string{quic.ConnectionIndexMetricLabel, commandMetricLabel}),
	}
	registerer.MustRegister(
		m.activeUDPFlows,
		m.totalUDPFlows,
		m.payloadTooLarge,
		m.retryFlowResponses,
		m.migratedFlows,
		m.unsupportedRemoteCommands,
	)
	return m
}
