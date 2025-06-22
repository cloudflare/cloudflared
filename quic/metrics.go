package quic

import (
	"reflect"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/quic-go/quic-go/logging"
	"github.com/rs/zerolog"
)

const (
	namespace                  = "quic"
	ConnectionIndexMetricLabel = "conn_index"
	frameTypeMetricLabel       = "frame_type"
	packetTypeMetricLabel      = "packet_type"
	reasonMetricLabel          = "reason"
)

var (
	clientMetrics = struct {
		totalConnections  prometheus.Counter
		closedConnections prometheus.Counter
		maxUDPPayloadSize *prometheus.GaugeVec
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
		mtu               *prometheus.GaugeVec
		congestionWindow  *prometheus.GaugeVec
		congestionState   *prometheus.GaugeVec
	}{
		totalConnections: prometheus.NewCounter(
			prometheus.CounterOpts{ //nolint:promlinter
				Namespace: namespace,
				Subsystem: "client",
				Name:      "total_connections",
				Help:      "Number of connections initiated",
			},
		),
		closedConnections: prometheus.NewCounter(
			prometheus.CounterOpts{ //nolint:promlinter
				Namespace: namespace,
				Subsystem: "client",
				Name:      "closed_connections",
				Help:      "Number of connections that has been closed",
			},
		),
		maxUDPPayloadSize: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "max_udp_payload",
				Help:      "Maximum UDP payload size in bytes for a QUIC packet",
			},
			[]string{ConnectionIndexMetricLabel},
		),
		sentFrames: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:promlinter
				Namespace: namespace,
				Subsystem: "client",
				Name:      "sent_frames",
				Help:      "Number of frames that have been sent through a connection",
			},
			[]string{ConnectionIndexMetricLabel, frameTypeMetricLabel},
		),
		sentBytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:promlinter
				Namespace: namespace,
				Subsystem: "client",
				Name:      "sent_bytes",
				Help:      "Number of bytes that have been sent through a connection",
			},
			[]string{ConnectionIndexMetricLabel},
		),
		receivedFrames: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:promlinter
				Namespace: namespace,
				Subsystem: "client",
				Name:      "received_frames",
				Help:      "Number of frames that have been received through a connection",
			},
			[]string{ConnectionIndexMetricLabel, frameTypeMetricLabel},
		),
		receivedBytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:promlinter
				Namespace: namespace,
				Subsystem: "client",
				Name:      "receive_bytes",
				Help:      "Number of bytes that have been received through a connection",
			},
			[]string{ConnectionIndexMetricLabel},
		),
		bufferedPackets: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:promlinter
				Namespace: namespace,
				Subsystem: "client",
				Name:      "buffered_packets",
				Help:      "Number of bytes that have been buffered on a connection",
			},
			[]string{ConnectionIndexMetricLabel, packetTypeMetricLabel},
		),
		droppedPackets: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:promlinter
				Namespace: namespace,
				Subsystem: "client",
				Name:      "dropped_packets",
				Help:      "Number of bytes that have been dropped on a connection",
			},
			[]string{ConnectionIndexMetricLabel, packetTypeMetricLabel, reasonMetricLabel},
		),
		lostPackets: prometheus.NewCounterVec(
			prometheus.CounterOpts{ //nolint:promlinter
				Namespace: namespace,
				Subsystem: "client",
				Name:      "lost_packets",
				Help:      "Number of packets that have been lost from a connection",
			},
			[]string{ConnectionIndexMetricLabel, reasonMetricLabel},
		),
		minRTT: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "min_rtt",
				Help:      "Lowest RTT measured on a connection in millisec",
			},
			[]string{ConnectionIndexMetricLabel},
		),
		latestRTT: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "latest_rtt",
				Help:      "Latest RTT measured on a connection",
			},
			[]string{ConnectionIndexMetricLabel},
		),
		smoothedRTT: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "smoothed_rtt",
				Help:      "Calculated smoothed RTT measured on a connection in millisec",
			},
			[]string{ConnectionIndexMetricLabel},
		),
		mtu: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "mtu",
				Help:      "Current maximum transmission unit (MTU) of a connection",
			},
			[]string{ConnectionIndexMetricLabel},
		),
		congestionWindow: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "congestion_window",
				Help:      "Current congestion window size",
			},
			[]string{ConnectionIndexMetricLabel},
		),
		congestionState: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "client",
				Name:      "congestion_state",
				Help:      "Current congestion control state. See https://pkg.go.dev/github.com/quic-go/quic-go@v0.45.0/logging#CongestionState for what each value maps to",
			},
			[]string{ConnectionIndexMetricLabel},
		),
	}

	registerClient = sync.Once{}

	packetTooBigDropped = prometheus.NewCounter(prometheus.CounterOpts{ //nolint:promlinter
		Namespace: namespace,
		Subsystem: "client",
		Name:      "packet_too_big_dropped",
		Help:      "Count of packets received from origin that are too big to send to the edge and are dropped as a result",
	})
)

type clientCollector struct {
	index  string
	logger *zerolog.Logger
}

func newClientCollector(index string, logger *zerolog.Logger) *clientCollector {
	registerClient.Do(func() {
		prometheus.MustRegister(
			clientMetrics.totalConnections,
			clientMetrics.closedConnections,
			clientMetrics.maxUDPPayloadSize,
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
			clientMetrics.mtu,
			clientMetrics.congestionWindow,
			clientMetrics.congestionState,
			packetTooBigDropped,
		)
	})

	return &clientCollector{
		index:  index,
		logger: logger,
	}
}

func (cc *clientCollector) startedConnection() {
	clientMetrics.totalConnections.Inc()
}

func (cc *clientCollector) closedConnection(error) {
	clientMetrics.closedConnections.Inc()
}

func (cc *clientCollector) receivedTransportParameters(params *logging.TransportParameters) {
	clientMetrics.maxUDPPayloadSize.WithLabelValues(cc.index).Set(float64(params.MaxUDPPayloadSize))
	cc.logger.Debug().Msgf("Received transport parameters: MaxUDPPayloadSize=%d, MaxIdleTimeout=%v, MaxDatagramFrameSize=%d", params.MaxUDPPayloadSize, params.MaxIdleTimeout, params.MaxDatagramFrameSize)
}

func (cc *clientCollector) sentPackets(size logging.ByteCount, frames []logging.Frame) {
	cc.collectPackets(size, frames, clientMetrics.sentFrames, clientMetrics.sentBytes, sent)
}

func (cc *clientCollector) receivedPackets(size logging.ByteCount, frames []logging.Frame) {
	cc.collectPackets(size, frames, clientMetrics.receivedFrames, clientMetrics.receivedBytes, received)
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

func (cc *clientCollector) updateCongestionWindow(size logging.ByteCount) {
	clientMetrics.congestionWindow.WithLabelValues(cc.index).Set(float64(size))
}

func (cc *clientCollector) updatedCongestionState(state logging.CongestionState) {
	clientMetrics.congestionState.WithLabelValues(cc.index).Set(float64(state))
}

func (cc *clientCollector) updateMTU(mtu logging.ByteCount) {
	clientMetrics.mtu.WithLabelValues(cc.index).Set(float64(mtu))
	cc.logger.Debug().Msgf("QUIC MTU updated to %d", mtu)
}

func (cc *clientCollector) collectPackets(size logging.ByteCount, frames []logging.Frame, counter, bandwidth *prometheus.CounterVec, direction direction) {
	for _, frame := range frames {
		switch f := frame.(type) {
		case logging.DataBlockedFrame:
			cc.logger.Debug().Msgf("%s data_blocked frame", direction)
		case logging.StreamDataBlockedFrame:
			cc.logger.Debug().Int64("streamID", int64(f.StreamID)).Msgf("%s stream_data_blocked frame", direction)
		}
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

type direction uint8

const (
	sent direction = iota
	received
)

func (d direction) String() string {
	if d == sent {
		return "sent"
	}
	return "received"
}
