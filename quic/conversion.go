package quic

import (
	"strconv"
	"time"

	"github.com/quic-go/quic-go/qlog"
)

// byteCountToPromCount converts an int64 byte count to float64 used in prometheus.
func byteCountToPromCount(count int64) float64 {
	return float64(count)
}

// durationToPromGauge converts a Duration to float64 milliseconds used in prometheus.
func durationToPromGauge(duration time.Duration) float64 {
	return float64(duration.Milliseconds())
}

// packetTypeString converts a qlog.PacketType to a Prometheus-safe label string.
// The allowlist prevents unbounded cardinality if upstream adds new values.
func packetTypeString(pt qlog.PacketType) string {
	switch pt {
	case qlog.PacketTypeInitial,
		qlog.PacketTypeHandshake,
		qlog.PacketType0RTT,
		qlog.PacketType1RTT,
		qlog.PacketTypeRetry,
		qlog.PacketTypeVersionNegotiation,
		qlog.PacketTypeStatelessReset:
		return string(pt)
	default:
		return "unknown_packet_type"
	}
}

// packetDropReasonString converts a qlog.PacketDropReason to a Prometheus-safe label string.
// The allowlist passes known values through and guards against unbounded cardinality.
func packetDropReasonString(reason qlog.PacketDropReason) string {
	switch reason {
	case qlog.PacketDropKeyUnavailable,
		qlog.PacketDropUnknownConnectionID,
		qlog.PacketDropHeaderParseError,
		qlog.PacketDropPayloadDecryptError,
		qlog.PacketDropProtocolViolation,
		qlog.PacketDropDOSPrevention,
		qlog.PacketDropUnsupportedVersion,
		qlog.PacketDropUnexpectedPacket,
		qlog.PacketDropUnexpectedSourceConnectionID,
		qlog.PacketDropUnexpectedVersion,
		qlog.PacketDropDuplicate:
		return string(reason)
	default:
		return "unknown_reason"
	}
}

// packetLossReasonString converts a qlog.PacketLossReason to a Prometheus-safe label string.
func packetLossReasonString(reason qlog.PacketLossReason) string {
	switch reason {
	case qlog.PacketLossReorderingThreshold,
		qlog.PacketLossTimeThreshold:
		return string(reason)
	default:
		return "unknown_loss_reason"
	}
}

// congestionStateToFloat maps a qlog.CongestionState string to a numeric value for prometheus gauges.
// Mapping: slow_start=0, congestion_avoidance=1, application_limited=2, recovery=3, unknown=-1.
func congestionStateToFloat(state qlog.CongestionState) float64 {
	switch state {
	case qlog.CongestionStateSlowStart:
		return 0
	case qlog.CongestionStateCongestionAvoidance:
		return 1
	case qlog.CongestionStateApplicationLimited:
		return 2
	case qlog.CongestionStateRecovery:
		return 3
	default:
		return -1
	}
}

func uint8ToString(input uint8) string {
	return strconv.FormatUint(uint64(input), 10)
}
