package qerr

import (
	"fmt"

	"github.com/lucas-clemente/quic-go/internal/qtls"
)

// ErrorCode can be used as a normal error without reason.
type ErrorCode uint64

// The error codes defined by QUIC
const (
	NoError                 ErrorCode = 0x0
	InternalError           ErrorCode = 0x1
	ConnectionRefused       ErrorCode = 0x2
	FlowControlError        ErrorCode = 0x3
	StreamLimitError        ErrorCode = 0x4
	StreamStateError        ErrorCode = 0x5
	FinalSizeError          ErrorCode = 0x6
	FrameEncodingError      ErrorCode = 0x7
	TransportParameterError ErrorCode = 0x8
	ConnectionIDLimitError  ErrorCode = 0x9
	ProtocolViolation       ErrorCode = 0xa
	InvalidToken            ErrorCode = 0xb
	ApplicationError        ErrorCode = 0xc
	CryptoBufferExceeded    ErrorCode = 0xd
	KeyUpdateError          ErrorCode = 0xe
	AEADLimitReached        ErrorCode = 0xf
	NoViablePathError       ErrorCode = 0x10
)

func (e ErrorCode) isCryptoError() bool {
	return e >= 0x100 && e < 0x200
}

func (e ErrorCode) Error() string {
	if e.isCryptoError() {
		return fmt.Sprintf("%s: %s", e.String(), e.Message())
	}
	return e.String()
}

// Message is a description of the error.
// It only returns a non-empty string for crypto errors.
func (e ErrorCode) Message() string {
	if !e.isCryptoError() {
		return ""
	}
	return qtls.Alert(e - 0x100).Error()
}

func (e ErrorCode) String() string {
	switch e {
	case NoError:
		return "NO_ERROR"
	case InternalError:
		return "INTERNAL_ERROR"
	case ConnectionRefused:
		return "CONNECTION_REFUSED"
	case FlowControlError:
		return "FLOW_CONTROL_ERROR"
	case StreamLimitError:
		return "STREAM_LIMIT_ERROR"
	case StreamStateError:
		return "STREAM_STATE_ERROR"
	case FinalSizeError:
		return "FINAL_SIZE_ERROR"
	case FrameEncodingError:
		return "FRAME_ENCODING_ERROR"
	case TransportParameterError:
		return "TRANSPORT_PARAMETER_ERROR"
	case ConnectionIDLimitError:
		return "CONNECTION_ID_LIMIT_ERROR"
	case ProtocolViolation:
		return "PROTOCOL_VIOLATION"
	case InvalidToken:
		return "INVALID_TOKEN"
	case ApplicationError:
		return "APPLICATION_ERROR"
	case CryptoBufferExceeded:
		return "CRYPTO_BUFFER_EXCEEDED"
	case KeyUpdateError:
		return "KEY_UPDATE_ERROR"
	case AEADLimitReached:
		return "AEAD_LIMIT_REACHED"
	case NoViablePathError:
		return "NO_VIABLE_PATH"
	default:
		if e.isCryptoError() {
			return fmt.Sprintf("CRYPTO_ERROR (%#x)", uint16(e))
		}
		return fmt.Sprintf("unknown error code: %#x", uint16(e))
	}
}
