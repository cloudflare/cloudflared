package origin

import (
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/h2mux"

	"github.com/prometheus/client_golang/prometheus"
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
}

type TunnelMetrics struct {
	haConnections prometheus.Gauge
	totalRequests prometheus.Counter
	requests      *prometheus.CounterVec
	// concurrentRequestsLock is a mutex for concurrentRequests and maxConcurrentRequests
	concurrentRequestsLock      sync.Mutex
	concurrentRequestsPerTunnel *prometheus.GaugeVec
	// concurrentRequests records count of concurrent requests for each tunnel, keyed by hash of label values
	concurrentRequests             map[uint64]uint64
	maxConcurrentRequestsPerTunnel *prometheus.GaugeVec
	// concurrentRequests records max count of concurrent requests for each tunnel, keyed by hash of label values
	maxConcurrentRequests map[uint64]uint64
	timerRetries          prometheus.Gauge

	reponses        *prometheus.CounterVec
	serverLocations *prometheus.GaugeVec
	// locationLock is a mutex for oldServerLocations
	locationLock sync.Mutex
	// oldServerLocations stores the last server the tunnel was connected to
	oldServerLocations map[uint64]string

	muxerMetrics *muxerMetrics
}

func newMuxerMetrics() *muxerMetrics {
	rtt := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_rtt",
			Help: "Round-trip time in millisecond",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(rtt)

	rttMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_rtt_min",
			Help: "Shortest round-trip time in millisecond",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(rttMin)

	rttMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_rtt_max",
			Help: "Longest round-trip time in millisecond",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(rttMax)

	receiveWindowAve := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_receive_window_ave",
			Help: "Average receive window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(receiveWindowAve)

	sendWindowAve := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_send_window_ave",
			Help: "Average send window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(sendWindowAve)

	receiveWindowMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_receive_window_min",
			Help: "Smallest receive window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(receiveWindowMin)

	receiveWindowMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_receive_window_max",
			Help: "Largest receive window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(receiveWindowMax)

	sendWindowMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_send_window_min",
			Help: "Smallest send window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(sendWindowMin)

	sendWindowMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_send_window_max",
			Help: "Largest send window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(sendWindowMax)

	inBoundRateCurr := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_inbound_bytes_per_sec_curr",
			Help: "Current inbounding bytes per second, 0 if there is no incoming connection",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(inBoundRateCurr)

	inBoundRateMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_inbound_bytes_per_sec_min",
			Help: "Minimum non-zero inbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(inBoundRateMin)

	inBoundRateMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_inbound_bytes_per_sec_max",
			Help: "Maximum inbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(inBoundRateMax)

	outBoundRateCurr := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_outbound_bytes_per_sec_curr",
			Help: "Current outbounding bytes per second, 0 if there is no outgoing traffic",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(outBoundRateCurr)

	outBoundRateMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_outbound_bytes_per_sec_min",
			Help: "Minimum non-zero outbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(outBoundRateMin)

	outBoundRateMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_outbound_bytes_per_sec_max",
			Help: "Maximum outbounding bytes per second",
		},
		[]string{"connection_id"},
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
func NewTunnelMetrics() *TunnelMetrics {
	haConnections := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "argotunnel_ha_connections",
			Help: "Number of active ha connections",
		})
	prometheus.MustRegister(haConnections)

	totalRequests := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "argotunnel_total_requests",
			Help: "Amount of requests proxied through all the tunnels",
		})
	prometheus.MustRegister(totalRequests)

	requestsPerTunnel := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "argotunnel_requests_per_tunnel",
			Help: "Amount of requests proxied through each tunnel",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(requestsPerTunnel)

	concurrentRequestsPerTunnel := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_concurrent_requests_per_tunnel",
			Help: "Concurrent requests proxied through each tunnel",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(concurrentRequestsPerTunnel)

	maxConcurrentRequestsPerTunnel := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_max_concurrent_requests_per_tunnel",
			Help: "Largest number of concurrent requests proxied through each tunnel so far",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(maxConcurrentRequestsPerTunnel)

	timerRetries := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "argotunnel_timer_retries",
			Help: "Unacknowledged heart beats count",
		})
	prometheus.MustRegister(timerRetries)

	// responseByCode := prometheus.NewCounterVec(
	// 	prometheus.CounterOpts{
	// 		Name: "argotunnel_response_by_code",
	// 		Help: "Count of responses by HTTP status code",
	// 	},
	// 	[]string{"status_code"},
	// )
	// prometheus.MustRegister(responseByCode)

	responseCodePerTunnel := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "argotunnel_response_code_per_tunnel",
			Help: "Count of responses by HTTP status code fore each tunnel",
		},
		[]string{"connection_id", "status_code"},
	)
	prometheus.MustRegister(responseCodePerTunnel)

	serverLocations := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "argotunnel_server_locations",
			Help: "Where each tunnel is connected to. 1 means current location, 0 means previous locations.",
		},
		[]string{"connection_id", "location"},
	)
	prometheus.MustRegister(serverLocations)

	return &TunnelMetrics{
		haConnections:                  haConnections,
		totalRequests:                  totalRequests,
		requests:                       requestsPerTunnel,
		concurrentRequestsPerTunnel:    concurrentRequestsPerTunnel,
		concurrentRequests:             make(map[uint64]uint64),
		maxConcurrentRequestsPerTunnel: maxConcurrentRequestsPerTunnel,
		maxConcurrentRequests:          make(map[uint64]uint64),
		timerRetries:                   timerRetries,

		reponses:           responseCodePerTunnel,
		serverLocations:    serverLocations,
		oldServerLocations: make(map[uint64]string),
		muxerMetrics:       newMuxerMetrics(),
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

func (t *TunnelMetrics) incrementRequests(metricLabelValues []string) {
	t.concurrentRequestsLock.Lock()
	var concurrentRequests uint64
	var ok bool
	hashKey := hashLabelValues(metricLabelValues)
	if concurrentRequests, ok = t.concurrentRequests[hashKey]; ok {
		t.concurrentRequests[hashKey] += 1
		concurrentRequests++
	} else {
		t.concurrentRequests[hashKey] = 1
		concurrentRequests = 1
	}
	if maxConcurrentRequests, ok := t.maxConcurrentRequests[hashKey]; (ok && maxConcurrentRequests < concurrentRequests) || !ok {
		t.maxConcurrentRequests[hashKey] = concurrentRequests
		t.maxConcurrentRequestsPerTunnel.WithLabelValues(metricLabelValues...).Set(float64(concurrentRequests))
	}
	t.concurrentRequestsLock.Unlock()

	t.totalRequests.Inc()
	t.requests.WithLabelValues(metricLabelValues...).Inc()
	t.concurrentRequestsPerTunnel.WithLabelValues(metricLabelValues...).Inc()
}

func (t *TunnelMetrics) decrementConcurrentRequests(metricLabelValues []string) {
	t.concurrentRequestsLock.Lock()
	hashKey := hashLabelValues(metricLabelValues)
	if _, ok := t.concurrentRequests[hashKey]; ok {
		t.concurrentRequests[hashKey] -= 1
	}
	t.concurrentRequestsLock.Unlock()

	t.concurrentRequestsPerTunnel.WithLabelValues(metricLabelValues...).Dec()
}

func (t *TunnelMetrics) incrementResponses(metricLabelValues []string, responseCode int) {
	labelValues := append(metricLabelValues, strconv.Itoa(responseCode))
	t.reponses.WithLabelValues(labelValues...).Inc()

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
