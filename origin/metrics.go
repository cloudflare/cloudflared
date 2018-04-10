package origin

import (
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
	haConnections     prometheus.Gauge
	totalRequests     prometheus.Counter
	requestsPerTunnel *prometheus.CounterVec
	// concurrentRequestsLock is a mutex for concurrentRequests and maxConcurrentRequests
	concurrentRequestsLock      sync.Mutex
	concurrentRequestsPerTunnel *prometheus.GaugeVec
	// concurrentRequests records count of concurrent requests for each tunnel
	concurrentRequests             map[string]uint64
	maxConcurrentRequestsPerTunnel *prometheus.GaugeVec
	// concurrentRequests records max count of concurrent requests for each tunnel
	maxConcurrentRequests map[string]uint64
	timerRetries          prometheus.Gauge
	responseByCode        *prometheus.CounterVec
	responseCodePerTunnel *prometheus.CounterVec
	serverLocations       *prometheus.GaugeVec
	// locationLock is a mutex for oldServerLocations
	locationLock sync.Mutex
	// oldServerLocations stores the last server the tunnel was connected to
	oldServerLocations map[string]string

	muxerMetrics *muxerMetrics
}

func newMuxerMetrics() *muxerMetrics {
	rtt := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "rtt",
			Help: "Round-trip time in millisecond",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(rtt)

	rttMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "rtt_min",
			Help: "Shortest round-trip time in millisecond",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(rttMin)

	rttMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "rtt_max",
			Help: "Longest round-trip time in millisecond",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(rttMax)

	receiveWindowAve := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "receive_window_ave",
			Help: "Average receive window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(receiveWindowAve)

	sendWindowAve := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "send_window_ave",
			Help: "Average send window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(sendWindowAve)

	receiveWindowMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "receive_window_min",
			Help: "Smallest receive window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(receiveWindowMin)

	receiveWindowMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "receive_window_max",
			Help: "Largest receive window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(receiveWindowMax)

	sendWindowMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "send_window_min",
			Help: "Smallest send window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(sendWindowMin)

	sendWindowMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "send_window_max",
			Help: "Largest send window size in bytes",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(sendWindowMax)

	inBoundRateCurr := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "inbound_bytes_per_sec_curr",
			Help: "Current inbounding bytes per second, 0 if there is no incoming connection",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(inBoundRateCurr)

	inBoundRateMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "inbound_bytes_per_sec_min",
			Help: "Minimum non-zero inbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(inBoundRateMin)

	inBoundRateMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "inbound_bytes_per_sec_max",
			Help: "Maximum inbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(inBoundRateMax)

	outBoundRateCurr := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "outbound_bytes_per_sec_curr",
			Help: "Current outbounding bytes per second, 0 if there is no outgoing traffic",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(outBoundRateCurr)

	outBoundRateMin := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "outbound_bytes_per_sec_min",
			Help: "Minimum non-zero outbounding bytes per second",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(outBoundRateMin)

	outBoundRateMax := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "outbound_bytes_per_sec_max",
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
}

func convertRTTMilliSec(t time.Duration) float64 {
	return float64(t / time.Millisecond)
}

// Metrics that can be collected without asking the edge
func NewTunnelMetrics() *TunnelMetrics {
	haConnections := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ha_connections",
			Help: "Number of active ha connections",
		})
	prometheus.MustRegister(haConnections)

	totalRequests := prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "total_requests",
			Help: "Amount of requests proxied through all the tunnels",
		})
	prometheus.MustRegister(totalRequests)

	requestsPerTunnel := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "requests_per_tunnel",
			Help: "Amount of requests proxied through each tunnel",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(requestsPerTunnel)

	concurrentRequestsPerTunnel := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "concurrent_requests_per_tunnel",
			Help: "Concurrent requests proxied through each tunnel",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(concurrentRequestsPerTunnel)

	maxConcurrentRequestsPerTunnel := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "max_concurrent_requests_per_tunnel",
			Help: "Largest number of concurrent requests proxied through each tunnel so far",
		},
		[]string{"connection_id"},
	)
	prometheus.MustRegister(maxConcurrentRequestsPerTunnel)

	timerRetries := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "timer_retries",
			Help: "Unacknowledged heart beats count",
		})
	prometheus.MustRegister(timerRetries)

	responseByCode := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "response_by_code",
			Help: "Count of responses by HTTP status code",
		},
		[]string{"status_code"},
	)
	prometheus.MustRegister(responseByCode)

	responseCodePerTunnel := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "response_code_per_tunnel",
			Help: "Count of responses by HTTP status code fore each tunnel",
		},
		[]string{"connection_id", "status_code"},
	)
	prometheus.MustRegister(responseCodePerTunnel)

	serverLocations := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "server_locations",
			Help: "Where each tunnel is connected to. 1 means current location, 0 means previous locations.",
		},
		[]string{"connection_id", "location"},
	)
	prometheus.MustRegister(serverLocations)

	return &TunnelMetrics{
		haConnections:                  haConnections,
		totalRequests:                  totalRequests,
		requestsPerTunnel:              requestsPerTunnel,
		concurrentRequestsPerTunnel:    concurrentRequestsPerTunnel,
		concurrentRequests:             make(map[string]uint64),
		maxConcurrentRequestsPerTunnel: maxConcurrentRequestsPerTunnel,
		maxConcurrentRequests:          make(map[string]uint64),
		timerRetries:                   timerRetries,
		responseByCode:                 responseByCode,
		responseCodePerTunnel:          responseCodePerTunnel,
		serverLocations:                serverLocations,
		oldServerLocations:             make(map[string]string),
		muxerMetrics:                   newMuxerMetrics(),
	}
}

func (t *TunnelMetrics) incrementHaConnections() {
	t.haConnections.Inc()
}

func (t *TunnelMetrics) decrementHaConnections() {
	t.haConnections.Dec()
}

func (t *TunnelMetrics) updateMuxerMetrics(connectionID string, metrics *h2mux.MuxerMetrics) {
	t.muxerMetrics.update(connectionID, metrics)
}

func (t *TunnelMetrics) incrementRequests(connectionID string) {
	t.concurrentRequestsLock.Lock()
	var concurrentRequests uint64
	var ok bool
	if concurrentRequests, ok = t.concurrentRequests[connectionID]; ok {
		t.concurrentRequests[connectionID] += 1
		concurrentRequests++
	} else {
		t.concurrentRequests[connectionID] = 1
		concurrentRequests = 1
	}
	if maxConcurrentRequests, ok := t.maxConcurrentRequests[connectionID]; (ok && maxConcurrentRequests < concurrentRequests) || !ok {
		t.maxConcurrentRequests[connectionID] = concurrentRequests
		t.maxConcurrentRequestsPerTunnel.WithLabelValues(connectionID).Set(float64(concurrentRequests))
	}
	t.concurrentRequestsLock.Unlock()

	t.totalRequests.Inc()
	t.requestsPerTunnel.WithLabelValues(connectionID).Inc()
	t.concurrentRequestsPerTunnel.WithLabelValues(connectionID).Inc()
}

func (t *TunnelMetrics) decrementConcurrentRequests(connectionID string) {
	t.concurrentRequestsLock.Lock()
	if _, ok := t.concurrentRequests[connectionID]; ok {
		t.concurrentRequests[connectionID] -= 1
	} else {
		Log.Error("Concurrent requests per tunnel metrics went wrong; you can't decrement concurrent requests count without increment it first.")
	}
	t.concurrentRequestsLock.Unlock()

	t.concurrentRequestsPerTunnel.WithLabelValues(connectionID).Dec()
}

func (t *TunnelMetrics) incrementResponses(connectionID, code string) {
	t.responseByCode.WithLabelValues(code).Inc()
	t.responseCodePerTunnel.WithLabelValues(connectionID, code).Inc()

}

func (t *TunnelMetrics) registerServerLocation(connectionID, loc string) {
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
