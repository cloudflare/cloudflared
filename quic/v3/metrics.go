package v3

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudflare/cloudflared/quic"
)

const (
	namespace      = "cloudflared"
	subsystem_udp  = "udp"
	subsystem_icmp = "icmp"

	commandMetricLabel = "command"
	reasonMetricLabel  = "reason"
)

type DroppedReason int

const (
	DroppedWriteFailed DroppedReason = iota
	DroppedWriteDeadlineExceeded
	DroppedWriteFull
	DroppedWriteFlowUnknown
	DroppedReadFailed
	// Origin payloads that are too large to proxy.
	DroppedReadTooLarge
)

var droppedReason = map[DroppedReason]string{
	DroppedWriteFailed:           "write_failed",
	DroppedWriteDeadlineExceeded: "write_deadline_exceeded",
	DroppedWriteFull:             "write_full",
	DroppedWriteFlowUnknown:      "write_flow_unknown",
	DroppedReadFailed:            "read_failed",
	DroppedReadTooLarge:          "read_too_large",
}

func (dr DroppedReason) String() string {
	return droppedReason[dr]
}

type Metrics interface {
	IncrementFlows(connIndex uint8)
	DecrementFlows(connIndex uint8)
	FailedFlow(connIndex uint8)
	RetryFlowResponse(connIndex uint8)
	MigrateFlow(connIndex uint8)
	UnsupportedRemoteCommand(connIndex uint8, command string)
	DroppedUDPDatagram(connIndex uint8, reason DroppedReason)
	DroppedICMPPackets(connIndex uint8, reason DroppedReason)
}

type metrics struct {
	activeUDPFlows            *prometheus.GaugeVec
	totalUDPFlows             *prometheus.CounterVec
	retryFlowResponses        *prometheus.CounterVec
	migratedFlows             *prometheus.CounterVec
	unsupportedRemoteCommands *prometheus.CounterVec
	droppedUDPDatagrams       *prometheus.CounterVec
	droppedICMPPackets        *prometheus.CounterVec
	failedFlows               *prometheus.CounterVec
}

func (m *metrics) IncrementFlows(connIndex uint8) {
	m.totalUDPFlows.WithLabelValues(fmt.Sprintf("%d", connIndex)).Inc()
	m.activeUDPFlows.WithLabelValues(fmt.Sprintf("%d", connIndex)).Inc()
}

func (m *metrics) DecrementFlows(connIndex uint8) {
	m.activeUDPFlows.WithLabelValues(fmt.Sprintf("%d", connIndex)).Dec()
}

func (m *metrics) FailedFlow(connIndex uint8) {
	m.failedFlows.WithLabelValues(fmt.Sprintf("%d", connIndex)).Inc()
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

func (m *metrics) DroppedUDPDatagram(connIndex uint8, reason DroppedReason) {
	m.droppedUDPDatagrams.WithLabelValues(fmt.Sprintf("%d", connIndex), reason.String()).Inc()
}

func (m *metrics) DroppedICMPPackets(connIndex uint8, reason DroppedReason) {
	m.droppedICMPPackets.WithLabelValues(fmt.Sprintf("%d", connIndex), reason.String()).Inc()
}

func NewMetrics(registerer prometheus.Registerer) Metrics {
	m := &metrics{
		activeUDPFlows: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem_udp,
			Name:      "active_flows",
			Help:      "Concurrent count of UDP flows that are being proxied to any origin",
		}, []string{quic.ConnectionIndexMetricLabel}),
		totalUDPFlows: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem_udp,
			Name:      "total_flows",
			Help:      "Total count of UDP flows that have been proxied to any origin",
		}, []string{quic.ConnectionIndexMetricLabel}),
		failedFlows: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem_udp,
			Name:      "failed_flows",
			Help:      "Total count of flows that errored and closed",
		}, []string{quic.ConnectionIndexMetricLabel}),
		retryFlowResponses: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem_udp,
			Name:      "retry_flow_responses",
			Help:      "Total count of UDP flows that have had to send their registration response more than once",
		}, []string{quic.ConnectionIndexMetricLabel}),
		migratedFlows: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem_udp,
			Name:      "migrated_flows",
			Help:      "Total count of UDP flows have been migrated across local connections",
		}, []string{quic.ConnectionIndexMetricLabel}),
		unsupportedRemoteCommands: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem_udp,
			Name:      "unsupported_remote_command_total",
			Help:      "Total count of unsupported remote RPC commands called",
		}, []string{quic.ConnectionIndexMetricLabel, commandMetricLabel}),
		droppedUDPDatagrams: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem_udp,
			Name:      "dropped_datagrams",
			Help:      "Total count of UDP dropped datagrams",
		}, []string{quic.ConnectionIndexMetricLabel, reasonMetricLabel}),
		droppedICMPPackets: prometheus.NewCounterVec(prometheus.CounterOpts{ //nolint:promlinter
			Namespace: namespace,
			Subsystem: subsystem_icmp,
			Name:      "dropped_packets",
			Help:      "Total count of ICMP dropped datagrams",
		}, []string{quic.ConnectionIndexMetricLabel, reasonMetricLabel}),
	}
	registerer.MustRegister(
		m.activeUDPFlows,
		m.totalUDPFlows,
		m.failedFlows,
		m.retryFlowResponses,
		m.migratedFlows,
		m.unsupportedRemoteCommands,
		m.droppedUDPDatagrams,
		m.droppedICMPPackets,
	)
	return m
}
