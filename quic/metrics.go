package quic

import (
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/quic-go/quic-go/qlog"
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
				Help:      "Current congestion control state (0=slow_start, 1=congestion_avoidance, 2=application_limited, 3=recovery, -1=unknown)",
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

func (cc *clientCollector) closedConnection() {
	clientMetrics.closedConnections.Inc()
}

// receivedTransportParameters records metrics from the peer's transport parameters.
func (cc *clientCollector) receivedTransportParameters(maxUDPPayloadSize int64, maxIdleTimeout time.Duration, maxDatagramFrameSize int64) {
	clientMetrics.maxUDPPayloadSize.WithLabelValues(cc.index).Set(float64(maxUDPPayloadSize))
	cc.logger.
		Debug().
		Int64("MaxUDPPayloadSize", maxUDPPayloadSize).
		Dur("MaxIdleTimeout", maxIdleTimeout).
		Int64("MaxDatagramFrameSize", maxDatagramFrameSize).Msgf("Received transport parameters")
}

// sentPackets records metrics for sent packets.
func (cc *clientCollector) sentPackets(size int64, frames []qlog.Frame) {
	cc.collectPackets(size, frames, clientMetrics.sentFrames, clientMetrics.sentBytes, sent)
}

// receivedPackets records metrics for received packets.
func (cc *clientCollector) receivedPackets(size int64, frames []qlog.Frame) {
	cc.collectPackets(size, frames, clientMetrics.receivedFrames, clientMetrics.receivedBytes, received)
}

// bufferedPackets records metrics for buffered packets.
func (cc *clientCollector) bufferedPackets(packetType qlog.PacketType) {
	clientMetrics.bufferedPackets.WithLabelValues(cc.index, packetTypeString(packetType)).Inc()
}

// droppedPackets records metrics for dropped packets.
func (cc *clientCollector) droppedPackets(packetType qlog.PacketType, size int64, reason qlog.PacketDropReason) {
	clientMetrics.droppedPackets.WithLabelValues(
		cc.index,
		packetTypeString(packetType),
		packetDropReasonString(reason),
	).Add(byteCountToPromCount(size))
}

// lostPackets records metrics for lost packets.
func (cc *clientCollector) lostPackets(reason qlog.PacketLossReason) {
	clientMetrics.lostPackets.WithLabelValues(cc.index, packetLossReasonString(reason)).Inc()
}

// updatedRTT records RTT metrics.
func (cc *clientCollector) updatedRTT(m qlog.MetricsUpdated) {
	clientMetrics.minRTT.WithLabelValues(cc.index).Set(durationToPromGauge(m.MinRTT))
	clientMetrics.latestRTT.WithLabelValues(cc.index).Set(durationToPromGauge(m.LatestRTT))
	clientMetrics.smoothedRTT.WithLabelValues(cc.index).Set(durationToPromGauge(m.SmoothedRTT))
}

// updateCongestionWindow records the congestion window size.
func (cc *clientCollector) updateCongestionWindow(size int64) {
	clientMetrics.congestionWindow.WithLabelValues(cc.index).Set(float64(size))
}

// updatedCongestionState records the congestion control state.
func (cc *clientCollector) updatedCongestionState(state qlog.CongestionState) {
	clientMetrics.congestionState.WithLabelValues(cc.index).Set(congestionStateToFloat(state))
}

// updateMTU records the MTU value.
func (cc *clientCollector) updateMTU(mtu int64) {
	clientMetrics.mtu.WithLabelValues(cc.index).Set(float64(mtu))
	cc.logger.Debug().Msgf("QUIC MTU updated to %d", mtu)
}

// collectPackets is the shared implementation for sentPackets and receivedPackets.
func (cc *clientCollector) collectPackets(size int64, frames []qlog.Frame, counter, bandwidth *prometheus.CounterVec, direction direction) {
	for _, frame := range frames {
		// qlog.Frame.Frame holds the concrete wire frame type as any.
		// The quic-go encoder always stores pointers (*wire.XxxFrame).
		switch f := frame.Frame.(type) {
		case *qlog.DataBlockedFrame:
			cc.logger.Debug().Int64("limit", int64(f.MaximumData)).Msgf("%s data_blocked frame", direction)
		case *qlog.StreamDataBlockedFrame:
			cc.logger.Debug().Int64("streamID", int64(f.StreamID)).Msgf("%s stream_data_blocked frame", direction)
		}
		counter.WithLabelValues(cc.index, frameName(frame)).Inc()
	}
	bandwidth.WithLabelValues(cc.index).Add(byteCountToPromCount(size))
}

// frameName extracts the type name from a qlog.Frame for use as a Prometheus label.
func frameName(frame qlog.Frame) string {
	if frame.Frame == nil {
		return "nil"
	}
	t := reflect.TypeOf(frame.Frame)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return strings.TrimSuffix(t.Name(), "Frame")
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
