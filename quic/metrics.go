package quic

import (
	"reflect"
	"strings"
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
		sentFrames        *prometheus.CounterVec
		sentBytes         *prometheus.CounterVec
		receivedFrames    *prometheus.CounterVec
		receivedBytes     *prometheus.CounterVec
		bufferedPackets   *prometheus.CounterVec
		droppedPackets    *prometheus.CounterVec
		lostPackets       *prometheus.CounterVec
		minRTT            *prometheus.GaugeVec
		latestRTT         *prometheus.GaugeVec
		smoothedRTT       *prometheus.GaugeVec
	}{
		totalConnections: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "total_connections",
				Help:      "Number of connections initiated",
			},
		),
		closedConnections: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "closed_connections",
				Help:      "Number of connections that has been closed",
			},
		),
		sentFrames: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "sent_frames",
				Help:      "Number of frames that have been sent through a connection",
			},
			append(clientConnLabels, "frame_type"),
		),
		sentBytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "sent_bytes",
				Help:      "Number of bytes that have been sent through a connection",
			},
			clientConnLabels,
		),
		receivedFrames: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "received_frames",
				Help:      "Number of frames that have been received through a connection",
			},
			append(clientConnLabels, "frame_type"),
		),
		receivedBytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "receive_bytes",
				Help:      "Number of bytes that have been received through a connection",
			},
			clientConnLabels,
		),
		bufferedPackets: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "buffered_packets",
				Help:      "Number of bytes that have been buffered on a connection",
			},
			append(clientConnLabels, "packet_type"),
		),
		droppedPackets: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "dropped_packets",
				Help:      "Number of bytes that have been dropped on a connection",
			},
			append(clientConnLabels, "packet_type", "reason"),
		),
		lostPackets: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "lost_packets",
				Help:      "Number of packets that have been lost from a connection",
			},
			append(clientConnLabels, "reason"),
		),
		minRTT: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "min_rtt",
				Help:      "Lowest RTT measured on a connection in millisec",
			},
			clientConnLabels,
		),
		latestRTT: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "latest_rtt",
				Help:      "Latest RTT measured on a connection",
			},
			clientConnLabels,
		),
		smoothedRTT: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "smoothed_rtt",
				Help:      "Calculated smoothed RTT measured on a connection in millisec",
			},
			clientConnLabels,
		),
	}

	registerClient = sync.Once{}

	packetTooBigDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "client",
		Name:      "packet_too_big_dropped",
		Help:      "Count of packets received from origin that are too big to send to the edge and are dropped as a result",
	})
)

type clientCollector struct {
	index string
}

func newClientCollector(index uint8) *clientCollector {
	registerClient.Do(func() {
		prometheus.MustRegister(
			clientMetrics.totalConnections,
			clientMetrics.closedConnections,
			clientMetrics.sentFrames,
			clientMetrics.sentBytes,
			clientMetrics.receivedFrames,
			clientMetrics.receivedBytes,
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

func (cc *clientCollector) sentPackets(size logging.ByteCount, frames []logging.Frame) {
	cc.collectPackets(size, frames, clientMetrics.sentFrames, clientMetrics.sentBytes)
}

func (cc *clientCollector) receivedPackets(size logging.ByteCount, frames []logging.Frame) {
	cc.collectPackets(size, frames, clientMetrics.receivedFrames, clientMetrics.receivedBytes)
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

func (cc *clientCollector) collectPackets(size logging.ByteCount, frames []logging.Frame, counter, bandwidth *prometheus.CounterVec) {
	for _, frame := range frames {
		counter.WithLabelValues(cc.index, frameName(frame)).Inc()
	}
	bandwidth.WithLabelValues(cc.index).Add(byteCountToPromCount(size))
}

func frameName(frame logging.Frame) string {
	if frame == nil {
		return "nil"
	} else {
		name := reflect.TypeOf(frame).Elem().Name()
		return strings.TrimSuffix(name, "Frame")
	}
}
