package origin

import (
	"sync"

	"github.com/cloudflare/cloudflare-warp/h2mux"

	log "github.com/Sirupsen/logrus"
	"github.com/prometheus/client_golang/prometheus"
)

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
	rtt                   prometheus.Gauge
	rttMin                prometheus.Gauge
	rttMax                prometheus.Gauge
	timerRetries          prometheus.Gauge
	receiveWindowSizeAve  prometheus.Gauge
	sendWindowSizeAve     prometheus.Gauge
	receiveWindowSizeMin  prometheus.Gauge
	receiveWindowSizeMax  prometheus.Gauge
	sendWindowSizeMin     prometheus.Gauge
	sendWindowSizeMax     prometheus.Gauge
	responseByCode        *prometheus.CounterVec
	responseCodePerTunnel *prometheus.CounterVec
	serverLocations       *prometheus.GaugeVec
	// locationLock is a mutex for oldServerLocations
	locationLock sync.Mutex
	// oldServerLocations stores the last server the tunnel was connected to
	oldServerLocations map[string]string
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

	rtt := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "rtt",
			Help: "Round-trip time",
		})
	prometheus.MustRegister(rtt)

	rttMin := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "rtt_min",
			Help: "Shortest round-trip time",
		})
	prometheus.MustRegister(rttMin)

	rttMax := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "rtt_max",
			Help: "Longest round-trip time",
		})
	prometheus.MustRegister(rttMax)

	timerRetries := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "timer_retries",
			Help: "Unacknowledged heart beats count",
		})
	prometheus.MustRegister(timerRetries)

	receiveWindowSizeAve := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "receive_window_ave",
			Help: "Average receive window size",
		})
	prometheus.MustRegister(receiveWindowSizeAve)

	sendWindowSizeAve := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "send_window_ave",
			Help: "Average send window size",
		})
	prometheus.MustRegister(sendWindowSizeAve)

	receiveWindowSizeMin := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "receive_window_min",
			Help: "Smallest receive window size",
		})
	prometheus.MustRegister(receiveWindowSizeMin)

	receiveWindowSizeMax := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "receive_window_max",
			Help: "Largest receive window size",
		})
	prometheus.MustRegister(receiveWindowSizeMax)

	sendWindowSizeMin := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "send_window_min",
			Help: "Smallest send window size",
		})
	prometheus.MustRegister(sendWindowSizeMin)

	sendWindowSizeMax := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "send_window_max",
			Help: "Largest send window size",
		})
	prometheus.MustRegister(sendWindowSizeMax)

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
		rtt:                   rtt,
		rttMin:                rttMin,
		rttMax:                rttMax,
		timerRetries:          timerRetries,
		receiveWindowSizeAve:  receiveWindowSizeAve,
		sendWindowSizeAve:     sendWindowSizeAve,
		receiveWindowSizeMin:  receiveWindowSizeMin,
		receiveWindowSizeMax:  receiveWindowSizeMax,
		sendWindowSizeMin:     sendWindowSizeMin,
		sendWindowSizeMax:     sendWindowSizeMax,
		responseByCode:        responseByCode,
		responseCodePerTunnel: responseCodePerTunnel,
		serverLocations:       serverLocations,
		oldServerLocations:    make(map[string]string),
	}
}

func (t *TunnelMetrics) incrementHaConnections() {
	t.haConnections.Inc()
}

func (t *TunnelMetrics) decrementHaConnections() {
	t.haConnections.Dec()
}

func (t *TunnelMetrics) updateTunnelFlowControlMetrics(metrics *h2mux.FlowControlMetrics) {
	t.receiveWindowSizeAve.Set(float64(metrics.AverageReceiveWindowSize))
	t.sendWindowSizeAve.Set(float64(metrics.AverageSendWindowSize))
	t.receiveWindowSizeMin.Set(float64(metrics.MinReceiveWindowSize))
	t.receiveWindowSizeMax.Set(float64(metrics.MaxReceiveWindowSize))
	t.sendWindowSizeMin.Set(float64(metrics.MinSendWindowSize))
	t.sendWindowSizeMax.Set(float64(metrics.MaxSendWindowSize))
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
		log.Error("Concurrent requests per tunnel metrics went wrong; you can't decrement concurrent requests count without increment it first.")
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
