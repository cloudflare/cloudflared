package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricsNamespace = "cloudflared"
	rpcSubsystem     = "rpc"
)

// CloudflaredServer operation labels
// CloudflaredServer is an extension of SessionManager with additional methods, but it's helpful
// to visualize it separately in the metrics since they are technically different client/servers.
const (
	Cloudflared = "cloudflared"
)

// ConfigurationManager operation labels
const (
	ConfigurationManager = "config"

	OperationUpdateConfiguration = "update_configuration"
)

// SessionManager operation labels
const (
	SessionManager = "session"

	OperationRegisterUdpSession   = "register_udp_session"
	OperationUnregisterUdpSession = "unregister_udp_session"
)

// RegistrationServer operation labels
const (
	Registration = "registration"

	OperationRegisterConnection       = "register_connection"
	OperationUnregisterConnection     = "unregister_connection"
	OperationUpdateLocalConfiguration = "update_local_configuration"
)

type rpcMetrics struct {
	serverOperations        *prometheus.CounterVec
	serverFailures          *prometheus.CounterVec
	serverOperationsLatency *prometheus.HistogramVec

	ClientOperations        *prometheus.CounterVec
	ClientFailures          *prometheus.CounterVec
	ClientOperationsLatency *prometheus.HistogramVec
}

var CapnpMetrics *rpcMetrics = &rpcMetrics{
	serverOperations: prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: rpcSubsystem,
			Name:      "server_operations",
			Help:      "Number of rpc methods by handler served",
		},
		[]string{"handler", "method"},
	),
	serverFailures: prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: rpcSubsystem,
			Name:      "server_failures",
			Help:      "Number of rpc methods failures by handler served",
		},
		[]string{"handler", "method"},
	),
	serverOperationsLatency: prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: rpcSubsystem,
			Name:      "server_latency_secs",
			Help:      "Latency of rpc methods by handler served",
			// Bucket starts at 50ms, each bucket grows by a factor of 3, up to 5 buckets and is expressed as seconds:
			// 50ms, 150ms, 450ms, 1350ms, 4050ms
			Buckets: prometheus.ExponentialBuckets(0.05, 3, 5),
		},
		[]string{"handler", "method"},
	),
	ClientOperations: prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: rpcSubsystem,
			Name:      "client_operations",
			Help:      "Number of rpc methods by handler requested",
		},
		[]string{"handler", "method"},
	),
	ClientFailures: prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: rpcSubsystem,
			Name:      "client_failures",
			Help:      "Number of rpc method failures by handler requested",
		},
		[]string{"handler", "method"},
	),
	ClientOperationsLatency: prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: rpcSubsystem,
			Name:      "client_latency_secs",
			Help:      "Latency of rpc methods by handler requested",
			// Bucket starts at 50ms, each bucket grows by a factor of 3, up to 5 buckets and is expressed as seconds:
			// 50ms, 150ms, 450ms, 1350ms, 4050ms
			Buckets: prometheus.ExponentialBuckets(0.05, 3, 5),
		},
		[]string{"handler", "method"},
	),
}

func ObserveServerHandler(inner func() error, handler, method string) error {
	defer CapnpMetrics.serverOperations.WithLabelValues(handler, method).Inc()
	timer := prometheus.NewTimer(prometheus.ObserverFunc(func(s float64) {
		CapnpMetrics.serverOperationsLatency.WithLabelValues(handler, method).Observe(s)
	}))
	defer timer.ObserveDuration()

	err := inner()
	if err != nil {
		CapnpMetrics.serverFailures.WithLabelValues(handler, method).Inc()
	}
	return err
}

func NewClientOperationLatencyObserver(server string, method string) *prometheus.Timer {
	return prometheus.NewTimer(prometheus.ObserverFunc(func(s float64) {
		CapnpMetrics.ClientOperationsLatency.WithLabelValues(server, method).Observe(s)
	}))
}

func init() {
	prometheus.MustRegister(CapnpMetrics.serverOperations)
	prometheus.MustRegister(CapnpMetrics.serverFailures)
	prometheus.MustRegister(CapnpMetrics.serverOperationsLatency)
	prometheus.MustRegister(CapnpMetrics.ClientOperations)
	prometheus.MustRegister(CapnpMetrics.ClientFailures)
	prometheus.MustRegister(CapnpMetrics.ClientOperationsLatency)
}
