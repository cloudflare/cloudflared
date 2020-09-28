package h2mux

import (
	"fmt"

	"golang.org/x/net/http2"
)

var (
	// HTTP2 error codes: https://http2.github.io/http2-spec/#ErrorCodes
	ErrHandshakeTimeout               = MuxerHandshakeError{"1000 handshake timeout"}
	ErrBadHandshakeNotSettings        = MuxerHandshakeError{"1001 unexpected response"}
	ErrBadHandshakeUnexpectedAck      = MuxerHandshakeError{"1002 unexpected response"}
	ErrBadHandshakeNoMagic            = MuxerHandshakeError{"1003 unexpected response"}
	ErrBadHandshakeWrongMagic         = MuxerHandshakeError{"1004 connected to endpoint of wrong type"}
	ErrBadHandshakeNotSettingsAck     = MuxerHandshakeError{"1005 unexpected response"}
	ErrBadHandshakeUnexpectedSettings = MuxerHandshakeError{"1006 unexpected response"}

	ErrUnexpectedFrameType = MuxerProtocolError{"2001 unexpected frame type", http2.ErrCodeProtocol}
	ErrUnknownStream       = MuxerProtocolError{"2002 unknown stream", http2.ErrCodeProtocol}
	ErrInvalidStream       = MuxerProtocolError{"2003 invalid stream", http2.ErrCodeProtocol}
	ErrNotRPCStream        = MuxerProtocolError{"2004 not RPC stream", http2.ErrCodeProtocol}

	ErrStreamHeadersSent               = MuxerApplicationError{"3000 headers already sent"}
	ErrStreamRequestConnectionClosed   = MuxerApplicationError{"3001 connection closed while opening stream"}
	ErrConnectionDropped               = MuxerApplicationError{"3002 connection dropped"}
	ErrStreamRequestTimeout            = MuxerApplicationError{"3003 open stream timeout"}
	ErrResponseHeadersTimeout          = MuxerApplicationError{"3004 timeout waiting for initial response headers"}
	ErrResponseHeadersConnectionClosed = MuxerApplicationError{"3005 connection closed while waiting for initial response headers"}

	ErrClosedStream = MuxerStreamError{"4000 stream closed", http2.ErrCodeStreamClosed}
)

type MuxerHandshakeError struct {
	cause string
}

func (e MuxerHandshakeError) Error() string {
	return fmt.Sprintf("Handshake error: %s", e.cause)
}

type MuxerProtocolError struct {
	cause  string
	h2code http2.ErrCode
}

func (e MuxerProtocolError) Error() string {
	return fmt.Sprintf("Protocol error: %s", e.cause)
}

type MuxerApplicationError struct {
	cause string
}

func (e MuxerApplicationError) Error() string {
	return fmt.Sprintf("Application error: %s", e.cause)
}

type MuxerStreamError struct {
	cause  string
	h2code http2.ErrCode
}

func (e MuxerStreamError) Error() string {
	return fmt.Sprintf("Stream error: %s", e.cause)
}
