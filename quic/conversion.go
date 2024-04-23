package quic

import (
	"strconv"
	"time"

	"github.com/quic-go/quic-go/logging"
)

// Helper to convert logging.ByteCount(alias for int64) to float64 used in prometheus
func byteCountToPromCount(count logging.ByteCount) float64 {
	return float64(count)
}

// Helper to convert Duration to float64 used in prometheus
func durationToPromGauge(duration time.Duration) float64 {
	return float64(duration.Milliseconds())
}

// Helper to convert https://pkg.go.dev/github.com/quic-go/quic-go@v0.23.0/logging#PacketType into string
func packetTypeString(pt logging.PacketType) string {
	switch pt {
	case logging.PacketTypeInitial:
		return "initial"
	case logging.PacketTypeHandshake:
		return "handshake"
	case logging.PacketTypeRetry:
		return "retry"
	case logging.PacketType0RTT:
		return "0_rtt"
	case logging.PacketTypeVersionNegotiation:
		return "version_negotiation"
	case logging.PacketType1RTT:
		return "1_rtt"
	case logging.PacketTypeStatelessReset:
		return "stateless_reset"
	case logging.PacketTypeNotDetermined:
		return "undetermined"
	default:
		return "unknown_packet_type"
	}
}

// Helper to convert https://pkg.go.dev/github.com/quic-go/quic-go@v0.23.0/logging#PacketDropReason into string
func packetDropReasonString(reason logging.PacketDropReason) string {
	switch reason {
	case logging.PacketDropKeyUnavailable:
		return "key_unavailable"
	case logging.PacketDropUnknownConnectionID:
		return "unknown_conn_id"
	case logging.PacketDropHeaderParseError:
		return "header_parse_err"
	case logging.PacketDropPayloadDecryptError:
		return "payload_decrypt_err"
	case logging.PacketDropProtocolViolation:
		return "protocol_violation"
	case logging.PacketDropDOSPrevention:
		return "dos_prevention"
	case logging.PacketDropUnsupportedVersion:
		return "unsupported_version"
	case logging.PacketDropUnexpectedPacket:
		return "unexpected_packet"
	case logging.PacketDropUnexpectedSourceConnectionID:
		return "unexpected_src_conn_id"
	case logging.PacketDropUnexpectedVersion:
		return "unexpected_version"
	case logging.PacketDropDuplicate:
		return "duplicate"
	default:
		return "unknown_reason"
	}
}

// Helper to convert https://pkg.go.dev/github.com/quic-go/quic-go@v0.23.0/logging#PacketLossReason into string
func packetLossReasonString(reason logging.PacketLossReason) string {
	switch reason {
	case logging.PacketLossReorderingThreshold:
		return "reordering"
	case logging.PacketLossTimeThreshold:
		return "timeout"
	default:
		return "unknown_loss_reason"
	}
}

func uint8ToString(input uint8) string {
	return strconv.FormatUint(uint64(input), 10)
}
