package quic

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/quic-go/quic-go/logging"
)

const (
	namespace = "quic"
)

var (
	clientConnLabels = []string{"conn_index"}
	clientMetrics    = struct {
		totalConnections  prometheus.Counter
		closedConnections prometheus.Counter
		sentPackets       *prometheus.CounterVec
		sentBytes         *prometheus.CounterVec
		receivePackets    *prometheus.CounterVec
		receiveBytes      *prometheus.CounterVec
		bufferedPackets   *prometheus.CounterVec
		droppedPackets    *prometheus.CounterVec
		lostPackets       *prometheus.CounterVec
		minRTT            *prometheus.GaugeVec
		latestRTT         *prometheus.GaugeVec
		smoothedRTT       *prometheus.GaugeVec
	}{
		totalConnections: prometheus.NewCounter(
			totalConnectionsOpts(logging.PerspectiveClient),
		),
		closedConnections: prometheus.NewCounter(
			closedConnectionsOpts(logging.PerspectiveClient),
		),
		sentPackets: prometheus.NewCounterVec(
			sentPacketsOpts(logging.PerspectiveClient),
			clientConnLabels,
		),
		sentBytes: prometheus.NewCounterVec(
			sentBytesOpts(logging.PerspectiveClient),
			clientConnLabels,
		),
		receivePackets: prometheus.NewCounterVec(
			receivePacketsOpts(logging.PerspectiveClient),
			clientConnLabels,
		),
		receiveBytes: prometheus.NewCounterVec(
			receiveBytesOpts(logging.PerspectiveClient),
			clientConnLabels,
		),
		bufferedPackets: prometheus.NewCounterVec(
			bufferedPacketsOpts(logging.PerspectiveClient),
			append(clientConnLabels, "packet_type"),
		),
		droppedPackets: prometheus.NewCounterVec(
			droppedPacketsOpts(logging.PerspectiveClient),
			append(clientConnLabels, "packet_type", "reason"),
		),
		lostPackets: prometheus.NewCounterVec(
			lostPacketsOpts(logging.PerspectiveClient),
			append(clientConnLabels, "reason"),
		),
		minRTT: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: perspectiveString(logging.PerspectiveClient),
				Name:      "min_rtt",
				Help:      "Lowest RTT measured on a connection in millisec",
			},
			clientConnLabels,
		),
		latestRTT: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: perspectiveString(logging.PerspectiveClient),
				Name:      "latest_rtt",
				Help:      "Latest RTT measured on a connection",
			},
			clientConnLabels,
		),
		smoothedRTT: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: perspectiveString(logging.PerspectiveClient),
				Name:      "smoothed_rtt",
				Help:      "Calculated smoothed RTT measured on a connection in millisec",
			},
			clientConnLabels,
		),
	}
	// The server has many QUIC connections. Adding per connection label incurs high memory cost
	serverMetrics = struct {
		totalConnections  prometheus.Counter
		closedConnections prometheus.Counter
		sentPackets       prometheus.Counter
		sentBytes         prometheus.Counter
		receivePackets    prometheus.Counter
		receiveBytes      prometheus.Counter
		bufferedPackets   *prometheus.CounterVec
		droppedPackets    *prometheus.CounterVec
		lostPackets       *prometheus.CounterVec
		rtt               prometheus.Histogram
	}{
		totalConnections: prometheus.NewCounter(
			totalConnectionsOpts(logging.PerspectiveServer),
		),
		closedConnections: prometheus.NewCounter(
			closedConnectionsOpts(logging.PerspectiveServer),
		),
		sentPackets: prometheus.NewCounter(
			sentPacketsOpts(logging.PerspectiveServer),
		),
		sentBytes: prometheus.NewCounter(
			sentBytesOpts(logging.PerspectiveServer),
		),
		receivePackets: prometheus.NewCounter(
			receivePacketsOpts(logging.PerspectiveServer),
		),
		receiveBytes: prometheus.NewCounter(
			receiveBytesOpts(logging.PerspectiveServer),
		),
		bufferedPackets: prometheus.NewCounterVec(
			bufferedPacketsOpts(logging.PerspectiveServer),
			[]string{"packet_type"},
		),
		droppedPackets: prometheus.NewCounterVec(
			droppedPacketsOpts(logging.PerspectiveServer),
			[]string{"packet_type", "reason"},
		),
		lostPackets: prometheus.NewCounterVec(
			lostPacketsOpts(logging.PerspectiveServer),
			[]string{"reason"},
		),
		rtt: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: perspectiveString(logging.PerspectiveServer),
				Name:      "rtt",
				Buckets:   []float64{5, 10, 20, 30, 40, 50, 75, 100},
			},
		),
	}
	registerClient = sync.Once{}
	registerServer = sync.Once{}

	packetTooBigDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(logging.PerspectiveClient),
		Name:      "packet_too_big_dropped",
		Help:      "Count of packets received from origin that are too big to send to the edge and are dropped as a result",
	})
)

// MetricsCollector abstracts the difference between client and server metrics from connTracer
type MetricsCollector interface {
	startedConnection()
	closedConnection(err error)
	sentPackets(logging.ByteCount)
	receivedPackets(logging.ByteCount)
	bufferedPackets(logging.PacketType)
	droppedPackets(logging.PacketType, logging.ByteCount, logging.PacketDropReason)
	lostPackets(logging.PacketLossReason)
	updatedRTT(*logging.RTTStats)
}

func totalConnectionsOpts(p logging.Perspective) prometheus.CounterOpts {
	var help string
	if p == logging.PerspectiveClient {
		help = "Number of connections initiated. For all quic metrics, client means the side initiating the connection"
	} else {
		help = "Number of connections accepted. For all quic metrics, server means the side accepting connections"
	}
	return prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(p),
		Name:      "total_connections",
		Help:      help,
	}
}

func closedConnectionsOpts(p logging.Perspective) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(p),
		Name:      "closed_connections",
		Help:      "Number of connections that has been closed",
	}
}

func sentPacketsOpts(p logging.Perspective) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(p),
		Name:      "sent_packets",
		Help:      "Number of packets that have been sent through a connection",
	}
}

func sentBytesOpts(p logging.Perspective) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(p),
		Name:      "sent_bytes",
		Help:      "Number of bytes that have been sent through a connection",
	}
}

func receivePacketsOpts(p logging.Perspective) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(p),
		Name:      "receive_packets",
		Help:      "Number of packets that have been received through a connection",
	}
}

func receiveBytesOpts(p logging.Perspective) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(p),
		Name:      "receive_bytes",
		Help:      "Number of bytes that have been received through a connection",
	}
}

func bufferedPacketsOpts(p logging.Perspective) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(p),
		Name:      "buffered_packets",
		Help:      "Number of bytes that have been buffered on a connection",
	}
}

func droppedPacketsOpts(p logging.Perspective) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(p),
		Name:      "dropped_packets",
		Help:      "Number of bytes that have been dropped on a connection",
	}
}

func lostPacketsOpts(p logging.Perspective) prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: perspectiveString(p),
		Name:      "lost_packets",
		Help:      "Number of packets that have been lost from a connection",
	}
}

type clientCollector struct {
	index string
}

func newClientCollector(index uint8) MetricsCollector {
	registerClient.Do(func() {
		prometheus.MustRegister(
			clientMetrics.totalConnections,
			clientMetrics.closedConnections,
			clientMetrics.sentPackets,
			clientMetrics.sentBytes,
			clientMetrics.receivePackets,
			clientMetrics.receiveBytes,
			clientMetrics.bufferedPackets,
			clientMetrics.droppedPackets,
			clientMetrics.lostPackets,
			clientMetrics.minRTT,
			clientMetrics.latestRTT,
			clientMetrics.smoothedRTT,
			packetTooBigDropped,
		)
	})
	return &clientCollector{
		index: uint8ToString(index),
	}
}

func (cc *clientCollector) startedConnection() {
	clientMetrics.totalConnections.Inc()
}

func (cc *clientCollector) closedConnection(err error) {
	clientMetrics.closedConnections.Inc()
}

func (cc *clientCollector) sentPackets(size logging.ByteCount) {
	clientMetrics.sentPackets.WithLabelValues(cc.index).Inc()
	clientMetrics.sentBytes.WithLabelValues(cc.index).Add(byteCountToPromCount(size))
}

func (cc *clientCollector) receivedPackets(size logging.ByteCount) {
	clientMetrics.receivePackets.WithLabelValues(cc.index).Inc()
	clientMetrics.receiveBytes.WithLabelValues(cc.index).Add(byteCountToPromCount(size))
}

func (cc *clientCollector) bufferedPackets(packetType logging.PacketType) {
	clientMetrics.bufferedPackets.WithLabelValues(cc.index, packetTypeString(packetType)).Inc()
}

func (cc *clientCollector) droppedPackets(packetType logging.PacketType, size logging.ByteCount, reason logging.PacketDropReason) {
	clientMetrics.droppedPackets.WithLabelValues(
		cc.index,
		packetTypeString(packetType),
		packetDropReasonString(reason),
	).Add(byteCountToPromCount(size))
}

func (cc *clientCollector) lostPackets(reason logging.PacketLossReason) {
	clientMetrics.lostPackets.WithLabelValues(cc.index, packetLossReasonString(reason)).Inc()
}

func (cc *clientCollector) updatedRTT(rtt *logging.RTTStats) {
	clientMetrics.minRTT.WithLabelValues(cc.index).Set(durationToPromGauge(rtt.MinRTT()))
	clientMetrics.latestRTT.WithLabelValues(cc.index).Set(durationToPromGauge(rtt.LatestRTT()))
	clientMetrics.smoothedRTT.WithLabelValues(cc.index).Set(durationToPromGauge(rtt.SmoothedRTT()))
}

type serverCollector struct{}

func newServiceCollector() MetricsCollector {
	registerServer.Do(func() {
		prometheus.MustRegister(
			serverMetrics.totalConnections,
			serverMetrics.closedConnections,
			serverMetrics.sentPackets,
			serverMetrics.sentBytes,
			serverMetrics.receivePackets,
			serverMetrics.receiveBytes,
			serverMetrics.bufferedPackets,
			serverMetrics.droppedPackets,
			serverMetrics.lostPackets,
			serverMetrics.rtt,
		)
	})
	return &serverCollector{}
}

func (sc *serverCollector) startedConnection() {
	serverMetrics.totalConnections.Inc()
}

func (sc *serverCollector) closedConnection(err error) {
	serverMetrics.closedConnections.Inc()
}

func (sc *serverCollector) sentPackets(size logging.ByteCount) {
	serverMetrics.sentPackets.Inc()
	serverMetrics.sentBytes.Add(byteCountToPromCount(size))
}

func (sc *serverCollector) receivedPackets(size logging.ByteCount) {
	serverMetrics.receivePackets.Inc()
	serverMetrics.receiveBytes.Add(byteCountToPromCount(size))
}

func (sc *serverCollector) bufferedPackets(packetType logging.PacketType) {
	serverMetrics.bufferedPackets.WithLabelValues(packetTypeString(packetType)).Inc()
}

func (sc *serverCollector) droppedPackets(packetType logging.PacketType, size logging.ByteCount, reason logging.PacketDropReason) {
	serverMetrics.droppedPackets.WithLabelValues(
		packetTypeString(packetType),
		packetDropReasonString(reason),
	).Add(byteCountToPromCount(size))
}

func (sc *serverCollector) lostPackets(reason logging.PacketLossReason) {
	serverMetrics.lostPackets.WithLabelValues(packetLossReasonString(reason)).Inc()
}

func (sc *serverCollector) updatedRTT(rtt *logging.RTTStats) {
	latestRTT := rtt.LatestRTT()
	// May return 0 if no valid updates have occurred
	if latestRTT > 0 {
		serverMetrics.rtt.Observe(durationToPromGauge(latestRTT))
	}
}
