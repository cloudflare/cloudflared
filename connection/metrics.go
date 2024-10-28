package connection

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	MetricsNamespace = "cloudflared"
	TunnelSubsystem  = "tunnel"
	muxerSubsystem   = "muxer"
	configSubsystem  = "config"
)

type localConfigMetrics struct {
	pushes       prometheus.Counter
	pushesErrors prometheus.Counter
}

type tunnelMetrics struct {
	serverLocations *prometheus.GaugeVec
	// locationLock is a mutex for oldServerLocations
	locationLock sync.Mutex
	// oldServerLocations stores the last server the tunnel was connected to
	oldServerLocations map[string]string

	regSuccess *prometheus.CounterVec
	regFail    *prometheus.CounterVec
	rpcFail    *prometheus.CounterVec

	tunnelsHA           tunnelsForHA
	userHostnamesCounts *prometheus.CounterVec

	localConfigMetrics *localConfigMetrics
}

func newLocalConfigMetrics() *localConfigMetrics {

	pushesMetric := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: configSubsystem,
			Name:      "local_config_pushes",
			Help:      "Number of local configuration pushes to the edge",
		},
	)

	pushesErrorsMetric := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: configSubsystem,
			Name:      "local_config_pushes_errors",
			Help:      "Number of errors occurred during local configuration pushes",
		},
	)

	prometheus.MustRegister(
		pushesMetric,
		pushesErrorsMetric,
	)

	return &localConfigMetrics{
		pushes:       pushesMetric,
		pushesErrors: pushesErrorsMetric,
	}
}

// Metrics that can be collected without asking the edge
func initTunnelMetrics() *tunnelMetrics {
	maxConcurrentRequestsPerTunnel := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: TunnelSubsystem,
			Name:      "max_concurrent_requests_per_tunnel",
			Help:      "Largest number of concurrent requests proxied through each tunnel so far",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(maxConcurrentRequestsPerTunnel)

	serverLocations := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: TunnelSubsystem,
			Name:      "server_locations",
			Help:      "Where each tunnel is connected to. 1 means current location, 0 means previous locations.",
		},
		[]string{"connection_id", "edge_location"},
	)
	prometheus.MustRegister(serverLocations)

	rpcFail := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: TunnelSubsystem,
			Name:      "tunnel_rpc_fail",
			Help:      "Count of RPC connection errors by type",
		},
		[]string{"error", "rpcName"},
	)
	prometheus.MustRegister(rpcFail)

	registerFail := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: TunnelSubsystem,
			Name:      "tunnel_register_fail",
			Help:      "Count of tunnel registration errors by type",
		},
		[]string{"error", "rpcName"},
	)
	prometheus.MustRegister(registerFail)

	userHostnamesCounts := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: TunnelSubsystem,
			Name:      "user_hostnames_counts",
			Help:      "Which user hostnames cloudflared is serving",
		},
		[]string{"userHostname"},
	)
	prometheus.MustRegister(userHostnamesCounts)

	registerSuccess := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: TunnelSubsystem,
			Name:      "tunnel_register_success",
			Help:      "Count of successful tunnel registrations",
		},
		[]string{"rpcName"},
	)
	prometheus.MustRegister(registerSuccess)

	return &tunnelMetrics{
		serverLocations:     serverLocations,
		oldServerLocations:  make(map[string]string),
		tunnelsHA:           newTunnelsForHA(),
		regSuccess:          registerSuccess,
		regFail:             registerFail,
		rpcFail:             rpcFail,
		userHostnamesCounts: userHostnamesCounts,
		localConfigMetrics:  newLocalConfigMetrics(),
	}
}

func (t *tunnelMetrics) registerServerLocation(connectionID, loc string) {
	t.locationLock.Lock()
	defer t.locationLock.Unlock()
	if oldLoc, ok := t.oldServerLocations[connectionID]; ok && oldLoc == loc {
		return
	} else if ok {
		t.serverLocations.WithLabelValues(connectionID, oldLoc).Dec()
	}
	t.serverLocations.WithLabelValues(connectionID, loc).Inc()
	t.oldServerLocations[connectionID] = loc
}

var tunnelMetricsInternal struct {
	sync.Once
	metrics *tunnelMetrics
}

func newTunnelMetrics() *tunnelMetrics {
	tunnelMetricsInternal.Do(func() {
		tunnelMetricsInternal.metrics = initTunnelMetrics()
	})
	return tunnelMetricsInternal.metrics
}
