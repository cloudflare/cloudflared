package origin

import (
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"

	"github.com/prometheus/client_golang/prometheus"
)

// ArgoTunnelNamespace is a namespace for metrics labels
const ArgoTunnelNamespace = "argotunnel"

// Lists of metrics label keys and values, in matched order
type MetricsLabelList struct {
	Keys   []string
	Values []string
}

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
}

type TunnelMetrics struct {
	haConnections prometheus.Gauge

	requests  *prometheus.CounterVec
	responses *prometheus.CounterVec

	// concurrentRequestsLock is a mutex for concurrentRequests and maxConcurrentRequests
	// counters are keyed by hash of label values
	concurrentRequestsLock       sync.Mutex
	concurrentRequests           *prometheus.GaugeVec
	concurrentRequestsCounter    map[uint64]uint64
	maxConcurrentRequests        *prometheus.GaugeVec
	maxConcurrentRequestsCounter map[uint64]uint64

	timerRetries prometheus.Gauge

	// oldServerLocations stores the last server the tunnel was connected to, secured by mutex
	locationLock       sync.Mutex
	serverLocations    *prometheus.GaugeVec
	oldServerLocations map[uint64]string

	muxerMetrics *muxerMetrics
}

func newMuxerMetrics(baseMetricsLabelKeys []string) *muxerMetrics {

	connectionIDLabelKeys := append(baseMetricsLabelKeys, "connection_id")

	rtt := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "rtt",
			Namespace: ArgoTunnelNamespace,
			Help:      "Round-trip time in millisecond",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(rtt)

	rttMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "rtt_min",
			Namespace: ArgoTunnelNamespace,
			Help:      "Shortest round-trip time in millisecond",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(rttMin)

	rttMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "rtt_max",
			Namespace: ArgoTunnelNamespace,
			Help:      "Longest round-trip time in millisecond",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(rttMax)

	receiveWindowAve := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "receive_window_avg",
			Namespace: ArgoTunnelNamespace,
			Help:      "Average receive window size in bytes",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(receiveWindowAve)

	sendWindowAve := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "send_window_avg",
			Namespace: ArgoTunnelNamespace,
			Help:      "Average send window size in bytes",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(sendWindowAve)

	receiveWindowMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "receive_window_min",
			Namespace: ArgoTunnelNamespace,
			Help:      "Smallest receive window size in bytes",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(receiveWindowMin)

	receiveWindowMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "receive_window_max",
			Namespace: ArgoTunnelNamespace,
			Help:      "Largest receive window size in bytes",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(receiveWindowMax)

	sendWindowMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "send_window_min",
			Namespace: ArgoTunnelNamespace,
			Help:      "Smallest send window size in bytes",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(sendWindowMin)

	sendWindowMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "send_window_max",
			Namespace: ArgoTunnelNamespace,
			Help:      "Largest send window size in bytes",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(sendWindowMax)

	inBoundRateCurr := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "inbound_bytes_per_sec_curr",
			Namespace: ArgoTunnelNamespace,
			Help:      "Current inbounding bytes per second, 0 if there is no incoming connection",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(inBoundRateCurr)

	inBoundRateMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "inbound_bytes_per_sec_min",
			Namespace: ArgoTunnelNamespace,
			Help:      "Minimum non-zero inbounding bytes per second",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(inBoundRateMin)

	inBoundRateMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "inbound_bytes_per_sec_max",
			Namespace: ArgoTunnelNamespace,
			Help:      "Maximum inbounding bytes per second",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(inBoundRateMax)

	outBoundRateCurr := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "outbound_bytes_per_sec_curr",
			Namespace: ArgoTunnelNamespace,
			Help:      "Current outbounding bytes per second, 0 if there is no outgoing traffic",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(outBoundRateCurr)

	outBoundRateMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "outbound_bytes_per_sec_min",
			Namespace: ArgoTunnelNamespace,
			Help:      "Minimum non-zero outbounding bytes per second",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(outBoundRateMin)

	outBoundRateMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "outbound_bytes_per_sec_max",
			Namespace: ArgoTunnelNamespace,
			Help:      "Maximum outbounding bytes per second",
		},
		connectionIDLabelKeys,
	)
	prometheus.MustRegister(outBoundRateMax)

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
	}
}

func (m *muxerMetrics) update(metricLabelValues []string, metrics *h2mux.MuxerMetrics) {
	m.rtt.WithLabelValues(metricLabelValues...).Set(convertRTTMilliSec(metrics.RTT))
	m.rttMin.WithLabelValues(metricLabelValues...).Set(convertRTTMilliSec(metrics.RTTMin))
	m.rttMax.WithLabelValues(metricLabelValues...).Set(convertRTTMilliSec(metrics.RTTMax))
	m.receiveWindowAve.WithLabelValues(metricLabelValues...).Set(metrics.ReceiveWindowAve)
	m.sendWindowAve.WithLabelValues(metricLabelValues...).Set(metrics.SendWindowAve)
	m.receiveWindowMin.WithLabelValues(metricLabelValues...).Set(float64(metrics.ReceiveWindowMin))
	m.receiveWindowMax.WithLabelValues(metricLabelValues...).Set(float64(metrics.ReceiveWindowMax))
	m.sendWindowMin.WithLabelValues(metricLabelValues...).Set(float64(metrics.SendWindowMin))
	m.sendWindowMax.WithLabelValues(metricLabelValues...).Set(float64(metrics.SendWindowMax))
	m.inBoundRateCurr.WithLabelValues(metricLabelValues...).Set(float64(metrics.InBoundRateCurr))
	m.inBoundRateMin.WithLabelValues(metricLabelValues...).Set(float64(metrics.InBoundRateMin))
	m.inBoundRateMax.WithLabelValues(metricLabelValues...).Set(float64(metrics.InBoundRateMax))
	m.outBoundRateCurr.WithLabelValues(metricLabelValues...).Set(float64(metrics.OutBoundRateCurr))
	m.outBoundRateMin.WithLabelValues(metricLabelValues...).Set(float64(metrics.OutBoundRateMin))
	m.outBoundRateMax.WithLabelValues(metricLabelValues...).Set(float64(metrics.OutBoundRateMax))
}

func convertRTTMilliSec(t time.Duration) float64 {
	return float64(t / time.Millisecond)
}

// Metrics that can be collected without asking the edge
func NewTunnelMetrics(baseMetricsLabelKeys []string) *TunnelMetrics {

	connectionIDLabels := append(baseMetricsLabelKeys, "connection_id")

	haConnections := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name:      "ha_connections",
			Namespace: ArgoTunnelNamespace,
			Help:      "Number of active ha connections",
		})
	prometheus.MustRegister(haConnections)

	requests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "requests",
			Namespace: ArgoTunnelNamespace,
			Help:      "Amount of requests proxied through each tunnel",
		},
		connectionIDLabels,
	)
	prometheus.MustRegister(requests)

	concurrentRequests := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "concurrent_requests",
			Namespace: ArgoTunnelNamespace,
			Help:      "Concurrent requests proxied through each tunnel",
		},
		connectionIDLabels,
	)
	prometheus.MustRegister(concurrentRequests)

	maxConcurrentRequests := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "max_concurrent_requests",
			Namespace: ArgoTunnelNamespace,
			Help:      "Largest number of concurrent requests proxied through each tunnel so far",
		},
		connectionIDLabels,
	)
	prometheus.MustRegister(maxConcurrentRequests)

	timerRetries := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name:      "timer_retries",
			Namespace: ArgoTunnelNamespace,
			Help:      "Unacknowledged heartbeat count",
		},
	)
	prometheus.MustRegister(timerRetries)

	responses := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:      "responses",
			Namespace: ArgoTunnelNamespace,
			Help:      "Count of responses for each tunnel",
		},
		append(baseMetricsLabelKeys, "connection_id", "status_code"),
	)
	prometheus.MustRegister(responses)

	serverLocations := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name:      "server_locations",
			Namespace: ArgoTunnelNamespace,
			Help:      "Where each tunnel is connected to. 1 means current location, 0 means previous locations.",
		},
		append(baseMetricsLabelKeys, "connection_id", "location"),
	)
	prometheus.MustRegister(serverLocations)

	return &TunnelMetrics{
		haConnections:                haConnections,
		requests:                     requests,
		concurrentRequests:           concurrentRequests,
		concurrentRequestsCounter:    make(map[uint64]uint64),
		maxConcurrentRequests:        maxConcurrentRequests,
		maxConcurrentRequestsCounter: make(map[uint64]uint64),
		timerRetries:                 timerRetries,

		responses:          responses,
		serverLocations:    serverLocations,
		oldServerLocations: make(map[uint64]string),
		muxerMetrics:       newMuxerMetrics(baseMetricsLabelKeys),
	}
}

func hashLabelValues(labelValues []string) uint64 {
	h := fnv.New64()
	for _, text := range labelValues {
		h.Write([]byte(text))
	}
	return h.Sum64()
}

func (t *TunnelMetrics) incrementHaConnections() {
	t.haConnections.Inc()
}

func (t *TunnelMetrics) decrementHaConnections() {
	t.haConnections.Dec()
}

func (t *TunnelMetrics) updateMuxerMetrics(metricLabelValues []string, metrics *h2mux.MuxerMetrics) {
	t.muxerMetrics.update(metricLabelValues, metrics)
}

// hashing of labels and locking are necessary in order to calculate concurrent requests
func (t *TunnelMetrics) incrementRequests(metricLabelValues []string) {
	t.concurrentRequestsLock.Lock()
	var concurrentRequests uint64
	var ok bool
	hashKey := hashLabelValues(metricLabelValues)
	if concurrentRequests, ok = t.concurrentRequestsCounter[hashKey]; ok {
		t.concurrentRequestsCounter[hashKey]++
		concurrentRequests++
	} else {
		t.concurrentRequestsCounter[hashKey] = 1
		concurrentRequests = 1
	}
	if maxConcurrentRequests, ok := t.maxConcurrentRequestsCounter[hashKey]; (ok && maxConcurrentRequests < concurrentRequests) || !ok {
		t.maxConcurrentRequestsCounter[hashKey] = concurrentRequests
		t.maxConcurrentRequests.WithLabelValues(metricLabelValues...).Set(float64(concurrentRequests))
	}
	t.concurrentRequestsLock.Unlock()

	t.requests.WithLabelValues(metricLabelValues...).Inc()
	t.concurrentRequests.WithLabelValues(metricLabelValues...).Inc()
}

func (t *TunnelMetrics) decrementConcurrentRequests(metricLabelValues []string) {
	t.concurrentRequestsLock.Lock()
	hashKey := hashLabelValues(metricLabelValues)
	if _, ok := t.concurrentRequestsCounter[hashKey]; ok {
		t.concurrentRequestsCounter[hashKey]--
	}
	t.concurrentRequestsLock.Unlock()

	t.concurrentRequests.WithLabelValues(metricLabelValues...).Dec()
}

func (t *TunnelMetrics) incrementResponses(metricLabelValues []string, responseCode int) {
	labelValues := append(metricLabelValues, strconv.Itoa(responseCode))
	t.responses.WithLabelValues(labelValues...).Inc()
}

func (t *TunnelMetrics) registerServerLocation(metricLabelValues []string, loc string) {
	t.locationLock.Lock()
	defer t.locationLock.Unlock()
	hashKey := hashLabelValues(metricLabelValues)
	if oldLoc, ok := t.oldServerLocations[hashKey]; ok && oldLoc == loc {
		return
	} else if ok {
		labelValues := append(metricLabelValues, oldLoc)
		t.serverLocations.WithLabelValues(labelValues...).Dec()
	}
	labelValues := append(metricLabelValues, loc)
	t.serverLocations.WithLabelValues(labelValues...).Inc()
	t.oldServerLocations[hashKey] = loc
}

// SetServerLocation is called by the tunnelHandler when the tunnel opens
func (t *TunnelMetrics) SetServerLocation(metricLabelValues []string, loc string) {
	labelValues := append(metricLabelValues, loc)
	t.serverLocations.WithLabelValues(labelValues...).Set(1)
}

// UnsetServerLocation is called by the tunnelHandler when the tunnel closes, or at least is known to be closed
func (t *TunnelMetrics) UnsetServerLocation(metricLabelValues []string, loc string) {
	labelValues := append(metricLabelValues, loc)
	t.serverLocations.WithLabelValues(labelValues...).Set(0)
}
