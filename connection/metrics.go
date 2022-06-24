package connection

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudflare/cloudflared/h2mux"
)

const (
	MetricsNamespace = "cloudflared"
	TunnelSubsystem  = "tunnel"
	muxerSubsystem   = "muxer"
	configSubsystem  = "config"
)

type muxerMetrics struct {
	rtt              *prometheus.GaugeVec
	rttMin           *prometheus.GaugeVec
	rttMax           *prometheus.GaugeVec
	receiveWindowAve *prometheus.GaugeVec
	sendWindowAve    *prometheus.GaugeVec
	receiveWindowMin *prometheus.GaugeVec
	receiveWindowMax *prometheus.GaugeVec
	sendWindowMin    *prometheus.GaugeVec
	sendWindowMax    *prometheus.GaugeVec
	inBoundRateCurr  *prometheus.GaugeVec
	inBoundRateMin   *prometheus.GaugeVec
	inBoundRateMax   *prometheus.GaugeVec
	outBoundRateCurr *prometheus.GaugeVec
	outBoundRateMin  *prometheus.GaugeVec
	outBoundRateMax  *prometheus.GaugeVec
	compBytesBefore  *prometheus.GaugeVec
	compBytesAfter   *prometheus.GaugeVec
	compRateAve      *prometheus.GaugeVec
}

type localConfigMetrics struct {
	pushes       prometheus.Counter
	pushesErrors prometheus.Counter
}

type tunnelMetrics struct {
	timerRetries    prometheus.Gauge
	serverLocations *prometheus.GaugeVec
	// locationLock is a mutex for oldServerLocations
	locationLock sync.Mutex
	// oldServerLocations stores the last server the tunnel was connected to
	oldServerLocations map[string]string

	regSuccess *prometheus.CounterVec
	regFail    *prometheus.CounterVec
	rpcFail    *prometheus.CounterVec

	muxerMetrics        *muxerMetrics
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

func newMuxerMetrics() *muxerMetrics {
	rtt := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "rtt",
			Help:      "Round-trip time in millisecond",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(rtt)

	rttMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "rtt_min",
			Help:      "Shortest round-trip time in millisecond",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(rttMin)

	rttMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "rtt_max",
			Help:      "Longest round-trip time in millisecond",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(rttMax)

	receiveWindowAve := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "receive_window_ave",
			Help:      "Average receive window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(receiveWindowAve)

	sendWindowAve := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "send_window_ave",
			Help:      "Average send window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(sendWindowAve)

	receiveWindowMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "receive_window_min",
			Help:      "Smallest receive window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(receiveWindowMin)

	receiveWindowMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "receive_window_max",
			Help:      "Largest receive window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(receiveWindowMax)

	sendWindowMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "send_window_min",
			Help:      "Smallest send window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(sendWindowMin)

	sendWindowMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "send_window_max",
			Help:      "Largest send window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(sendWindowMax)

	inBoundRateCurr := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "inbound_bytes_per_sec_curr",
			Help:      "Current inbounding bytes per second, 0 if there is no incoming connection",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(inBoundRateCurr)

	inBoundRateMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "inbound_bytes_per_sec_min",
			Help:      "Minimum non-zero inbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(inBoundRateMin)

	inBoundRateMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "inbound_bytes_per_sec_max",
			Help:      "Maximum inbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(inBoundRateMax)

	outBoundRateCurr := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "outbound_bytes_per_sec_curr",
			Help:      "Current outbounding bytes per second, 0 if there is no outgoing traffic",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(outBoundRateCurr)

	outBoundRateMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "outbound_bytes_per_sec_min",
			Help:      "Minimum non-zero outbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(outBoundRateMin)

	outBoundRateMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "outbound_bytes_per_sec_max",
			Help:      "Maximum outbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(outBoundRateMax)

	compBytesBefore := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "comp_bytes_before",
			Help:      "Bytes sent via cross-stream compression, pre compression",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(compBytesBefore)

	compBytesAfter := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "comp_bytes_after",
			Help:      "Bytes sent via cross-stream compression, post compression",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(compBytesAfter)

	compRateAve := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: muxerSubsystem,
			Name:      "comp_rate_ave",
			Help:      "Average outbound cross-stream compression ratio",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(compRateAve)

	return &muxerMetrics{
		rtt:              rtt,
		rttMin:           rttMin,
		rttMax:           rttMax,
		receiveWindowAve: receiveWindowAve,
		sendWindowAve:    sendWindowAve,
		receiveWindowMin: receiveWindowMin,
		receiveWindowMax: receiveWindowMax,
		sendWindowMin:    sendWindowMin,
		sendWindowMax:    sendWindowMax,
		inBoundRateCurr:  inBoundRateCurr,
		inBoundRateMin:   inBoundRateMin,
		inBoundRateMax:   inBoundRateMax,
		outBoundRateCurr: outBoundRateCurr,
		outBoundRateMin:  outBoundRateMin,
		outBoundRateMax:  outBoundRateMax,
		compBytesBefore:  compBytesBefore,
		compBytesAfter:   compBytesAfter,
		compRateAve:      compRateAve,
	}
}

func (m *muxerMetrics) update(connectionID string, metrics *h2mux.MuxerMetrics) {
	m.rtt.WithLabelValues(connectionID).Set(convertRTTMilliSec(metrics.RTT))
	m.rttMin.WithLabelValues(connectionID).Set(convertRTTMilliSec(metrics.RTTMin))
	m.rttMax.WithLabelValues(connectionID).Set(convertRTTMilliSec(metrics.RTTMax))
	m.receiveWindowAve.WithLabelValues(connectionID).Set(metrics.ReceiveWindowAve)
	m.sendWindowAve.WithLabelValues(connectionID).Set(metrics.SendWindowAve)
	m.receiveWindowMin.WithLabelValues(connectionID).Set(float64(metrics.ReceiveWindowMin))
	m.receiveWindowMax.WithLabelValues(connectionID).Set(float64(metrics.ReceiveWindowMax))
	m.sendWindowMin.WithLabelValues(connectionID).Set(float64(metrics.SendWindowMin))
	m.sendWindowMax.WithLabelValues(connectionID).Set(float64(metrics.SendWindowMax))
	m.inBoundRateCurr.WithLabelValues(connectionID).Set(float64(metrics.InBoundRateCurr))
	m.inBoundRateMin.WithLabelValues(connectionID).Set(float64(metrics.InBoundRateMin))
	m.inBoundRateMax.WithLabelValues(connectionID).Set(float64(metrics.InBoundRateMax))
	m.outBoundRateCurr.WithLabelValues(connectionID).Set(float64(metrics.OutBoundRateCurr))
	m.outBoundRateMin.WithLabelValues(connectionID).Set(float64(metrics.OutBoundRateMin))
	m.outBoundRateMax.WithLabelValues(connectionID).Set(float64(metrics.OutBoundRateMax))
	m.compBytesBefore.WithLabelValues(connectionID).Set(float64(metrics.CompBytesBefore.Value()))
	m.compBytesAfter.WithLabelValues(connectionID).Set(float64(metrics.CompBytesAfter.Value()))
	m.compRateAve.WithLabelValues(connectionID).Set(float64(metrics.CompRateAve()))
}

func convertRTTMilliSec(t time.Duration) float64 {
	return float64(t / time.Millisecond)
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

	timerRetries := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: TunnelSubsystem,
			Name:      "timer_retries",
			Help:      "Unacknowledged heart beats count",
		})
	prometheus.MustRegister(timerRetries)

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
		timerRetries:        timerRetries,
		serverLocations:     serverLocations,
		oldServerLocations:  make(map[string]string),
		muxerMetrics:        newMuxerMetrics(),
		tunnelsHA:           newTunnelsForHA(),
		regSuccess:          registerSuccess,
		regFail:             registerFail,
		rpcFail:             rpcFail,
		userHostnamesCounts: userHostnamesCounts,
		localConfigMetrics:  newLocalConfigMetrics(),
	}
}

func (t *tunnelMetrics) updateMuxerMetrics(connectionID string, metrics *h2mux.MuxerMetrics) {
	t.muxerMetrics.update(connectionID, metrics)
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
