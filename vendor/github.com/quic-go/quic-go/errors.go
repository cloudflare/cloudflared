package quic

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/qerr"
)

type (
	TransportError          = qerr.TransportError
	ApplicationError        = qerr.ApplicationError
	VersionNegotiationError = qerr.VersionNegotiationError
	StatelessResetError     = qerr.StatelessResetError
	IdleTimeoutError        = qerr.IdleTimeoutError
	HandshakeTimeoutError   = qerr.HandshakeTimeoutError
)

type (
	TransportErrorCode   = qerr.TransportErrorCode
	ApplicationErrorCode = qerr.ApplicationErrorCode
	StreamErrorCode      = qerr.StreamErrorCode
)

const (
	NoError                   = qerr.NoError
	InternalError             = qerr.InternalError
	ConnectionRefused         = qerr.ConnectionRefused
	FlowControlError          = qerr.FlowControlError
	StreamLimitError          = qerr.StreamLimitError
	StreamStateError          = qerr.StreamStateError
	FinalSizeError            = qerr.FinalSizeError
	FrameEncodingError        = qerr.FrameEncodingError
	TransportParameterError   = qerr.TransportParameterError
	ConnectionIDLimitError    = qerr.ConnectionIDLimitError
	ProtocolViolation         = qerr.ProtocolViolation
	InvalidToken              = qerr.InvalidToken
	ApplicationErrorErrorCode = qerr.ApplicationErrorErrorCode
	CryptoBufferExceeded      = qerr.CryptoBufferExceeded
	KeyUpdateError            = qerr.KeyUpdateError
	AEADLimitReached          = qerr.AEADLimitReached
	NoViablePathError         = qerr.NoViablePathError
)

// A StreamError is used for Stream.CancelRead and Stream.CancelWrite.
// It is also returned from Stream.Read and Stream.Write if the peer canceled reading or writing.
type StreamError struct {
	StreamID  StreamID
	ErrorCode StreamErrorCode
	Remote    bool
}

func (e *StreamError) Is(target error) bool {
	t, ok := target.(*StreamError)
	return ok && e.StreamID == t.StreamID && e.ErrorCode == t.ErrorCode && e.Remote == t.Remote
}

func (e *StreamError) Error() string {
	pers := "local"
	if e.Remote {
		pers = "remote"
	}
	return fmt.Sprintf("stream %d canceled by %s with error code %d", e.StreamID, pers, e.ErrorCode)
}

// DatagramTooLargeError is returned from Connection.SendDatagram if the payload is too large to be sent.
type DatagramTooLargeError struct {
	MaxDatagramPayloadSize int64
}

func (e *DatagramTooLargeError) Is(target error) bool {
	t, ok := target.(*DatagramTooLargeError)
	return ok && e.MaxDatagramPayloadSize == t.MaxDatagramPayloadSize
}

func (e *DatagramTooLargeError) Error() string { return "DATAGRAM frame too large" }
