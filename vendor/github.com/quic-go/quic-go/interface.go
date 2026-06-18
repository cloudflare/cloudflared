package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/logging"
)

// The StreamID is the ID of a QUIC stream.
type StreamID = protocol.StreamID

// A Version is a QUIC version number.
type Version = protocol.Version

const (
	// Version1 is RFC 9000
	Version1 = protocol.Version1
	// Version2 is RFC 9369
	Version2 = protocol.Version2
)

// A ClientToken is a token received by the client.
// It can be used to skip address validation on future connection attempts.
type ClientToken struct {
	data []byte
}

type TokenStore interface {
	// Pop searches for a ClientToken associated with the given key.
	// Since tokens are not supposed to be reused, it must remove the token from the cache.
	// It returns nil when no token is found.
	Pop(key string) (token *ClientToken)

	// Put adds a token to the cache with the given key. It might get called
	// multiple times in a connection.
	Put(key string, token *ClientToken)
}

// Err0RTTRejected is the returned from:
//   - Open{Uni}Stream{Sync}
//   - Accept{Uni}Stream
//   - Stream.Read and Stream.Write
//
// when the server rejects a 0-RTT connection attempt.
var Err0RTTRejected = errors.New("0-RTT rejected")

// ConnectionTracingKey can be used to associate a [logging.ConnectionTracer] with a [Connection].
// It is set on the Connection.Context() context,
// as well as on the context passed to logging.Tracer.NewConnectionTracer.
//
// Deprecated: Applications can set their own tracing key using Transport.ConnContext.
var ConnectionTracingKey = connTracingCtxKey{}

// ConnectionTracingID is the type of the context value saved under the ConnectionTracingKey.
//
// Deprecated: Applications can set their own tracing key using Transport.ConnContext.
type ConnectionTracingID uint64

type connTracingCtxKey struct{}

// QUICVersionContextKey can be used to find out the QUIC version of a TLS handshake from the
// context returned by tls.Config.ClientInfo.Context.
var QUICVersionContextKey = handshake.QUICVersionContextKey

// Stream is the interface implemented by QUIC streams.
// In addition to the errors listed on the [Connection],
// calls to stream functions can return a [StreamError] if the stream is canceled.
type Stream interface {
	ReceiveStream
	SendStream
	// SetDeadline sets the read and write deadlines associated
	// with the connection. It is equivalent to calling both
	// SetReadDeadline and SetWriteDeadline.
	SetDeadline(t time.Time) error
}

// A ReceiveStream is a unidirectional Receive Stream.
type ReceiveStream interface {
	// StreamID returns the stream ID.
	StreamID() StreamID
	// Read reads data from the stream.
	// Read can be made to time out using SetDeadline and SetReadDeadline.
	// If the stream was canceled, the error is a StreamError.
	io.Reader
	// CancelRead aborts receiving on this stream.
	// It will ask the peer to stop transmitting stream data.
	// Read will unblock immediately, and future Read calls will fail.
	// When called multiple times or after reading the io.EOF it is a no-op.
	CancelRead(StreamErrorCode)
	// SetReadDeadline sets the deadline for future Read calls and
	// any currently-blocked Read call.
	// A zero value for t means Read will not time out.
	SetReadDeadline(t time.Time) error
}

// A SendStream is a unidirectional Send Stream.
type SendStream interface {
	// StreamID returns the stream ID.
	StreamID() StreamID
	// Write writes data to the stream.
	// Write can be made to time out using SetDeadline and SetWriteDeadline.
	// If the stream was canceled, the error is a StreamError.
	io.Writer
	// Close closes the write-direction of the stream.
	// Future calls to Write are not permitted after calling Close.
	// It must not be called concurrently with Write.
	// It must not be called after calling CancelWrite.
	io.Closer
	// CancelWrite aborts sending on this stream.
	// Data already written, but not yet delivered to the peer is not guaranteed to be delivered reliably.
	// Write will unblock immediately, and future calls to Write will fail.
	// When called multiple times it is a no-op.
	// When called after Close, it aborts delivery. Note that there is no guarantee if
	// the peer will receive the FIN or the reset first.
	CancelWrite(StreamErrorCode)
	// The Context is canceled as soon as the write-side of the stream is closed.
	// This happens when Close() or CancelWrite() is called, or when the peer
	// cancels the read-side of their stream.
	// The cancellation cause is set to the error that caused the stream to
	// close, or `context.Canceled` in case the stream is closed without error.
	Context() context.Context
	// SetWriteDeadline sets the deadline for future Write calls
	// and any currently-blocked Write call.
	// Even if write times out, it may return n > 0, indicating that
	// some data was successfully written.
	// A zero value for t means Write will not time out.
	SetWriteDeadline(t time.Time) error
}

// A Connection is a QUIC connection between two peers.
// Calls to the connection (and to streams) can return the following types of errors:
//   - [ApplicationError]: for errors triggered by the application running on top of QUIC
//   - [TransportError]: for errors triggered by the QUIC transport (in many cases a misbehaving peer)
//   - [IdleTimeoutError]: when the peer goes away unexpectedly (this is a [net.Error] timeout error)
//   - [HandshakeTimeoutError]: when the cryptographic handshake takes too long (this is a [net.Error] timeout error)
//   - [StatelessResetError]: when we receive a stateless reset
//   - [VersionNegotiationError]: returned by the client, when there's no version overlap between the peers
type Connection interface {
	// AcceptStream returns the next stream opened by the peer, blocking until one is available.
	AcceptStream(context.Context) (Stream, error)
	// AcceptUniStream returns the next unidirectional stream opened by the peer, blocking until one is available.
	AcceptUniStream(context.Context) (ReceiveStream, error)
	// OpenStream opens a new bidirectional QUIC stream.
	// There is no signaling to the peer about new streams:
	// The peer can only accept the stream after data has been sent on the stream,
	// or the stream has been reset or closed.
	// When reaching the peer's stream limit, it is not possible to open a new stream until the
	// peer raises the stream limit. In that case, a StreamLimitReachedError is returned.
	OpenStream() (Stream, error)
	// OpenStreamSync opens a new bidirectional QUIC stream.
	// It blocks until a new stream can be opened.
	// There is no signaling to the peer about new streams:
	// The peer can only accept the stream after data has been sent on the stream,
	// or the stream has been reset or closed.
	OpenStreamSync(context.Context) (Stream, error)
	// OpenUniStream opens a new outgoing unidirectional QUIC stream.
	// There is no signaling to the peer about new streams:
	// The peer can only accept the stream after data has been sent on the stream,
	// or the stream has been reset or closed.
	// When reaching the peer's stream limit, it is not possible to open a new stream until the
	// peer raises the stream limit. In that case, a StreamLimitReachedError is returned.
	OpenUniStream() (SendStream, error)
	// OpenUniStreamSync opens a new outgoing unidirectional QUIC stream.
	// It blocks until a new stream can be opened.
	// There is no signaling to the peer about new streams:
	// The peer can only accept the stream after data has been sent on the stream,
	// or the stream has been reset or closed.
	OpenUniStreamSync(context.Context) (SendStream, error)
	// LocalAddr returns the local address.
	LocalAddr() net.Addr
	// RemoteAddr returns the address of the peer.
	RemoteAddr() net.Addr
	// CloseWithError closes the connection with an error.
	// The error string will be sent to the peer.
	CloseWithError(ApplicationErrorCode, string) error
	// Context returns a context that is cancelled when the connection is closed.
	// The cancellation cause is set to the error that caused the connection to
	// close, or `context.Canceled` in case the listener is closed first.
	Context() context.Context
	// ConnectionState returns basic details about the QUIC connection.
	// Warning: This API should not be considered stable and might change soon.
	ConnectionState() ConnectionState

	// SendDatagram sends a message using a QUIC datagram, as specified in RFC 9221.
	// There is no delivery guarantee for DATAGRAM frames, they are not retransmitted if lost.
	// The payload of the datagram needs to fit into a single QUIC packet.
	// In addition, a datagram may be dropped before being sent out if the available packet size suddenly decreases.
	// If the payload is too large to be sent at the current time, a DatagramTooLargeError is returned.
	SendDatagram(payload []byte) error
	// ReceiveDatagram gets a message received in a datagram, as specified in RFC 9221.
	ReceiveDatagram(context.Context) ([]byte, error)

	AddPath(*Transport) (*Path, error)
}

// An EarlyConnection is a connection that is handshaking.
// Data sent during the handshake is encrypted using the forward secure keys.
// When using client certificates, the client's identity is only verified
// after completion of the handshake.
type EarlyConnection interface {
	Connection

	// HandshakeComplete blocks until the handshake completes (or fails).
	// For the client, data sent before completion of the handshake is encrypted with 0-RTT keys.
	// For the server, data sent before completion of the handshake is encrypted with 1-RTT keys,
	// however the client's identity is only verified once the handshake completes.
	HandshakeComplete() <-chan struct{}

	NextConnection(context.Context) (Connection, error)
}

// StatelessResetKey is a key used to derive stateless reset tokens.
type StatelessResetKey [32]byte

// TokenGeneratorKey is a key used to encrypt session resumption tokens.
type TokenGeneratorKey = handshake.TokenProtectorKey

// A ConnectionID is a QUIC Connection ID, as defined in RFC 9000.
// It is not able to handle QUIC Connection IDs longer than 20 bytes,
// as they are allowed by RFC 8999.
type ConnectionID = protocol.ConnectionID

// ConnectionIDFromBytes interprets b as a [ConnectionID]. It panics if b is
// longer than 20 bytes.
func ConnectionIDFromBytes(b []byte) ConnectionID {
	return protocol.ParseConnectionID(b)
}

// A ConnectionIDGenerator allows the application to take control over the generation of Connection IDs.
// Connection IDs generated by an implementation must be of constant length.
type ConnectionIDGenerator interface {
	// GenerateConnectionID generates a new Connection ID.
	// Generated Connection IDs must be unique and observers should not be able to correlate two Connection IDs.
	GenerateConnectionID() (ConnectionID, error)

	// ConnectionIDLen returns the length of Connection IDs generated by this implementation.
	// Implementations must return constant-length Connection IDs with lengths between 0 and 20 bytes.
	// A length of 0 can only be used when an endpoint doesn't need to multiplex connections during migration.
	ConnectionIDLen() int
}

// Config contains all configuration data needed for a QUIC server or client.
type Config struct {
	// GetConfigForClient is called for incoming connections.
	// If the error is not nil, the connection attempt is refused.
	GetConfigForClient func(info *ClientInfo) (*Config, error)
	// The QUIC versions that can be negotiated.
	// If not set, it uses all versions available.
	Versions []Version
	// HandshakeIdleTimeout is the idle timeout before completion of the handshake.
	// If we don't receive any packet from the peer within this time, the connection attempt is aborted.
	// Additionally, if the handshake doesn't complete in twice this time, the connection attempt is also aborted.
	// If this value is zero, the timeout is set to 5 seconds.
	HandshakeIdleTimeout time.Duration
	// MaxIdleTimeout is the maximum duration that may pass without any incoming network activity.
	// The actual value for the idle timeout is the minimum of this value and the peer's.
	// This value only applies after the handshake has completed.
	// If the timeout is exceeded, the connection is closed.
	// If this value is zero, the timeout is set to 30 seconds.
	MaxIdleTimeout time.Duration
	// The TokenStore stores tokens received from the server.
	// Tokens are used to skip address validation on future connection attempts.
	// The key used to store tokens is the ServerName from the tls.Config, if set
	// otherwise the token is associated with the server's IP address.
	TokenStore TokenStore
	// InitialStreamReceiveWindow is the initial size of the stream-level flow control window for receiving data.
	// If the application is consuming data quickly enough, the flow control auto-tuning algorithm
	// will increase the window up to MaxStreamReceiveWindow.
	// If this value is zero, it will default to 512 KB.
	// Values larger than the maximum varint (quicvarint.Max) will be clipped to that value.
	InitialStreamReceiveWindow uint64
	// MaxStreamReceiveWindow is the maximum stream-level flow control window for receiving data.
	// If this value is zero, it will default to 6 MB.
	// Values larger than the maximum varint (quicvarint.Max) will be clipped to that value.
	MaxStreamReceiveWindow uint64
	// InitialConnectionReceiveWindow is the initial size of the stream-level flow control window for receiving data.
	// If the application is consuming data quickly enough, the flow control auto-tuning algorithm
	// will increase the window up to MaxConnectionReceiveWindow.
	// If this value is zero, it will default to 512 KB.
	// Values larger than the maximum varint (quicvarint.Max) will be clipped to that value.
	InitialConnectionReceiveWindow uint64
	// MaxConnectionReceiveWindow is the connection-level flow control window for receiving data.
	// If this value is zero, it will default to 15 MB.
	// Values larger than the maximum varint (quicvarint.Max) will be clipped to that value.
	MaxConnectionReceiveWindow uint64
	// AllowConnectionWindowIncrease is called every time the connection flow controller attempts
	// to increase the connection flow control window.
	// If set, the caller can prevent an increase of the window. Typically, it would do so to
	// limit the memory usage.
	// To avoid deadlocks, it is not valid to call other functions on the connection or on streams
	// in this callback.
	AllowConnectionWindowIncrease func(conn Connection, delta uint64) bool
	// MaxIncomingStreams is the maximum number of concurrent bidirectional streams that a peer is allowed to open.
	// If not set, it will default to 100.
	// If set to a negative value, it doesn't allow any bidirectional streams.
	// Values larger than 2^60 will be clipped to that value.
	MaxIncomingStreams int64
	// MaxIncomingUniStreams is the maximum number of concurrent unidirectional streams that a peer is allowed to open.
	// If not set, it will default to 100.
	// If set to a negative value, it doesn't allow any unidirectional streams.
	// Values larger than 2^60 will be clipped to that value.
	MaxIncomingUniStreams int64
	// KeepAlivePeriod defines whether this peer will periodically send a packet to keep the connection alive.
	// If set to 0, then no keep alive is sent. Otherwise, the keep alive is sent on that period (or at most
	// every half of MaxIdleTimeout, whichever is smaller).
	KeepAlivePeriod time.Duration
	// InitialPacketSize is the initial size (and the lower limit) for packets sent.
	// Under most circumstances, it is not necessary to manually set this value,
	// since path MTU discovery quickly finds the path's MTU.
	// If set too high, the path might not support packets of that size, leading to a timeout of the QUIC handshake.
	// Values below 1200 are invalid.
	InitialPacketSize uint16
	// DisablePathMTUDiscovery disables Path MTU Discovery (RFC 8899).
	// This allows the sending of QUIC packets that fully utilize the available MTU of the path.
	// Path MTU discovery is only available on systems that allow setting of the Don't Fragment (DF) bit.
	DisablePathMTUDiscovery bool
	// Allow0RTT allows the application to decide if a 0-RTT connection attempt should be accepted.
	// Only valid for the server.
	Allow0RTT bool
	// Enable QUIC datagram support (RFC 9221).
	EnableDatagrams bool
	Tracer          func(context.Context, logging.Perspective, ConnectionID) *logging.ConnectionTracer
}

// ClientHelloInfo contains information about an incoming connection attempt.
//
// Deprecated: Use ClientInfo instead.
type ClientHelloInfo = ClientInfo

// ClientInfo contains information about an incoming connection attempt.
type ClientInfo struct {
	// RemoteAddr is the remote address on the Initial packet.
	// Unless AddrVerified is set, the address is not yet verified, and could be a spoofed IP address.
	RemoteAddr net.Addr
	// AddrVerified says if the remote address was verified using QUIC's Retry mechanism.
	// Note that the Retry mechanism costs one network roundtrip,
	// and is not performed unless Transport.MaxUnvalidatedHandshakes is surpassed.
	AddrVerified bool
}

// ConnectionState records basic details about a QUIC connection.
type ConnectionState struct {
	// TLS contains information about the TLS connection state, incl. the tls.ConnectionState.
	TLS tls.ConnectionState
	// SupportsDatagrams indicates whether the peer advertised support for QUIC datagrams (RFC 9221).
	// When true, datagrams can be sent using the Connection's SendDatagram method.
	// This is a unilateral declaration by the peer - receiving datagrams is only possible if
	// datagram support was enabled locally via Config.EnableDatagrams.
	SupportsDatagrams bool
	// Used0RTT says if 0-RTT resumption was used.
	Used0RTT bool
	// Version is the QUIC version of the QUIC connection.
	Version Version
	// GSO says if generic segmentation offload is used.
	GSO bool
}
