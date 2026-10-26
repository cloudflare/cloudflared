package v3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

const (
	// A default is provided in the case that the client does not provide a close idle timeout.
	defaultCloseIdleAfter = 210 * time.Second

	// The maximum payload from the origin that we will be able to read. However, even though we will
	// read 1500 bytes from the origin, we limit the amount of bytes to be proxied to less than
	// this value (maxDatagramPayloadLen).
	maxOriginUDPPacketSize = 1500

	// The maximum amount of datagrams a session will queue up before it begins dropping datagrams.
	// This channel buffer is small because we assume that the dedicated writer to the origin is typically
	// fast enought to keep the channel empty.
	writeChanCapacity = 512

	logFlowID        = "flowID"
	logPacketSizeKey = "packetSize"
)

// SessionCloseErr indicates that the session's Close method was called.
var SessionCloseErr error = errors.New("flow was closed directly") //nolint:errname

// SessionIdleErr is returned when the session was closed because there was no communication
// in either direction over the session for the timeout period.
type SessionIdleErr struct { //nolint:errname
	timeout time.Duration
}

func (e SessionIdleErr) Error() string {
	return fmt.Sprintf("flow was idle for %v", e.timeout)
}

func (e SessionIdleErr) Is(target error) bool {
	_, ok := target.(SessionIdleErr)
	return ok
}

func newSessionIdleErr(timeout time.Duration) error {
	return SessionIdleErr{timeout}
}

type Session interface {
	io.Closer
	ID() RequestID
	ConnectionID() uint8
	RemoteAddr() net.Addr
	LocalAddr() net.Addr
	ResetIdleTimer()
	Migrate(eyeball DatagramConn, ctx context.Context, logger *zerolog.Logger)
	// Serve starts the event loop for processing UDP packets
	Serve(ctx context.Context) error
	Write(payload []byte)
}

type session struct {
	id             RequestID
	closeAfterIdle time.Duration
	origin         io.ReadWriteCloser
	originAddr     net.Addr
	localAddr      net.Addr
	eyeball        atomic.Pointer[DatagramConn]
	writeChan      chan []byte
	// activeAtChan is used to communicate the last read/write time
	activeAtChan chan time.Time
	errChan      chan error
	// The close channel signal only exists for the write loop because the read loop is always waiting on a read
	// from the UDP socket to the origin. To close the read loop we close the socket.
	// Additionally, we can't close the writeChan to indicate that writes are complete because the producer (edge)
	// side may still be trying to write to this session.
	closeWrite  chan struct{}
	contextChan chan context.Context
	metrics     Metrics
	log         *zerolog.Logger

	// A special close function that we wrap with sync.Once to make sure it is only called once
	closeFn func() error
}

func NewSession(
	id RequestID,
	closeAfterIdle time.Duration,
	origin io.ReadWriteCloser,
	originAddr net.Addr,
	localAddr net.Addr,
	eyeball DatagramConn,
	metrics Metrics,
	log *zerolog.Logger,
) Session {
	logger := log.With().Str(logFlowID, id.String()).Logger()
	writeChan := make(chan []byte, writeChanCapacity)
	// errChan has three slots to allow for all writers (the closeFn, the read loop and the write loop) to
	// write to the channel without blocking since there is only ever one value read from the errChan by the
	// waitForCloseCondition.
	errChan := make(chan error, 3)
	closeWrite := make(chan struct{})
	session := &session{
		id:             id,
		closeAfterIdle: closeAfterIdle,
		origin:         origin,
		originAddr:     originAddr,
		localAddr:      localAddr,
		eyeball:        atomic.Pointer[DatagramConn]{},
		writeChan:      writeChan,
		// activeAtChan has low capacity. It can be full when there are many concurrent read/write. markActive() will
		// drop instead of blocking because last active time only needs to be an approximation
		activeAtChan: make(chan time.Time, 1),
		errChan:      errChan,
		closeWrite:   closeWrite,
		// contextChan is an unbounded channel to help enforce one active migration of a session at a time.
		contextChan: make(chan context.Context),
		metrics:     metrics,
		log:         &logger,
		closeFn: sync.OnceValue(func() error {
			// We don't want to block on sending to the close channel if it is already full
			select {
			case errChan <- SessionCloseErr:
			default:
			}
			// Indicate to the write loop that the session is now closed
			close(closeWrite)
			// Close the socket directly to unblock the read loop and cause it to also end
			return origin.Close()
		}),
	}
	session.eyeball.Store(&eyeball)
	return session
}

func (s *session) ID() RequestID {
	return s.id
}

func (s *session) RemoteAddr() net.Addr {
	return s.originAddr
}

func (s *session) LocalAddr() net.Addr {
	return s.localAddr
}

func (s *session) ConnectionID() uint8 {
	eyeball := *(s.eyeball.Load())
	return eyeball.ID()
}

func (s *session) Migrate(eyeball DatagramConn, ctx context.Context, logger *zerolog.Logger) {
	current := *(s.eyeball.Load())
	// Only migrate if the connection ids are different.
	if current.ID() != eyeball.ID() {
		s.eyeball.Store(&eyeball)
		s.contextChan <- ctx
		log := logger.With().Str(logFlowID, s.id.String()).Logger()
		s.log = &log
	}
	// The session is already running so we want to restart the idle timeout since no proxied packets have come down yet.
	s.markActive()
	connectionIndex := eyeball.ID()
	s.metrics.MigrateFlow(connectionIndex)
}

func (s *session) Serve(ctx context.Context) error {
	go s.writeLoop()
	go s.readLoop()
	return s.waitForCloseCondition(ctx, s.closeAfterIdle)
}

// Read datagrams from the origin and write them to the connection.
func (s *session) readLoop() {
	// QUIC implementation copies data to another buffer before returning https://github.com/quic-go/quic-go/blob/v0.24.0/session.go#L1967-L1975
	// This makes it safe to share readBuffer between iterations
	readBuffer := [maxOriginUDPPacketSize + DatagramPayloadHeaderLen]byte{}
	// To perform a zero copy write when passing the datagram to the connection, we prepare the buffer with
	// the required datagram header information. We can reuse this buffer for this session since the header is the
	// same for the each read.
	_ = MarshalPayloadHeaderTo(s.id, readBuffer[:DatagramPayloadHeaderLen])
	for {
		// Read from the origin UDP socket
		n, err := s.origin.Read(readBuffer[DatagramPayloadHeaderLen:])
		if err != nil {
			if isConnectionClosed(err) {
				s.log.Debug().Msgf("flow (read) connection closed: %v", err)
			}
			s.closeSession(err)
			return
		}
		if n < 0 {
			s.metrics.DroppedUDPDatagram(s.ConnectionID(), DroppedReadFailed)
			s.log.Warn().Int(logPacketSizeKey, n).Msg("flow (origin) packet read was negative and was dropped")
			continue
		}
		if n > maxDatagramPayloadLen {
			s.metrics.DroppedUDPDatagram(s.ConnectionID(), DroppedReadTooLarge)
			s.log.Error().Int(logPacketSizeKey, n).Msg("flow (origin) packet read was too large and was dropped")
			continue
		}
		// We need to synchronize on the eyeball in-case that the connection was migrated. This should be rarely a point
		// of lock contention, as a migration can only happen during startup of a session before traffic flow.
		eyeball := *(s.eyeball.Load())
		// Sending a packet to the session does block on the [quic.Connection], however, this is okay because it
		// will cause back-pressure to the kernel buffer if the writes are not fast enough to the edge.
		err = eyeball.SendUDPSessionDatagram(readBuffer[:DatagramPayloadHeaderLen+n])
		if err != nil {
			s.closeSession(err)
			return
		}
		// Mark the session as active since we proxied a valid packet from the origin.
		s.markActive()
	}
}

func (s *session) Write(payload []byte) {
	select {
	case s.writeChan <- payload:
	default:
		s.metrics.DroppedUDPDatagram(s.ConnectionID(), DroppedWriteFull)
		s.log.Error().Msg("failed to write flow payload to origin: dropped")
	}
}

// Read datagrams from the write channel to the origin.
func (s *session) writeLoop() {
	for {
		select {
		case <-s.closeWrite:
			// When the closeWrite channel is closed, we will no longer write to the origin and end this
			// goroutine since the session is now closed.
			return
		case payload := <-s.writeChan:
			n, err := s.origin.Write(payload)
			if err != nil {
				// Check if this is a write deadline exceeded to the connection
				if errors.Is(err, os.ErrDeadlineExceeded) {
					s.metrics.DroppedUDPDatagram(s.ConnectionID(), DroppedWriteDeadlineExceeded)
					s.log.Warn().Err(err).Msg("flow (write) deadline exceeded: dropping packet")
					continue
				}
				if isConnectionClosed(err) {
					s.log.Debug().Msgf("flow (write) connection closed: %v", err)
				}
				s.log.Err(err).Msg("failed to write flow payload to origin")
				s.closeSession(err)
				// If we fail to write to the origin socket, we need to end the writer and close the session
				return
			}
			// Write must return a non-nil error if it returns n < len(p). https://pkg.go.dev/io#Writer
			if n < len(payload) {
				s.metrics.DroppedUDPDatagram(s.ConnectionID(), DroppedWriteFailed)
				s.log.Err(io.ErrShortWrite).Msg("failed to write the full flow payload to origin")
				continue
			}
			// Mark the session as active since we successfully proxied a packet to the origin.
			s.markActive()
		}
	}
}

func isConnectionClosed(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// Send an error to the error channel to report that an error has either happened on the tunnel or origin side of the
// proxied connection.
func (s *session) closeSession(err error) {
	select {
	case s.errChan <- err:
	default:
		// In the case that the errChan is already full, we will skip over it and return as to not block
		// the caller because we should start cleaning up the session.
		s.log.Warn().Msg("error channel was full")
	}
}

// ResetIdleTimer will restart the current idle timer.
//
// This public method is used to allow operators of sessions the ability to extend the session using information that is
// known external to the session itself.
func (s *session) ResetIdleTimer() {
	s.markActive()
}

// Sends the last active time to the idle checker loop without blocking. activeAtChan will only be full when there
// are many concurrent read/write. It is fine to lose some precision
func (s *session) markActive() {
	select {
	case s.activeAtChan <- time.Now():
	default:
	}
}

func (s *session) Close() error {
	// Make sure that we only close the origin connection once
	return s.closeFn()
}

func (s *session) waitForCloseCondition(ctx context.Context, closeAfterIdle time.Duration) error {
	connCtx := ctx
	// Closing the session at the end cancels read so Serve() can return, additionally, it closes the
	// closeWrite channel which indicates to the write loop to return.
	defer s.Close()
	if closeAfterIdle == 0 {
		// Provided that the default caller doesn't specify one
		closeAfterIdle = defaultCloseIdleAfter
	}

	checkIdleTimer := time.NewTimer(closeAfterIdle)
	defer checkIdleTimer.Stop()

	for {
		select {
		case <-connCtx.Done():
			return connCtx.Err()
		case newContext := <-s.contextChan:
			// During migration of a session, we need to make sure that the context of the new connection is used instead
			// of the old connection context. This will ensure that when the old connection goes away, this session will
			// still be active on the existing connection.
			connCtx = newContext
			continue
		case reason := <-s.errChan:
			// Any error returned here is from the read or write loops indicating that it can no longer process datagrams
			// and as such the session needs to close.
			s.metrics.FailedFlow(s.ConnectionID())
			return reason
		case <-checkIdleTimer.C:
			// The check idle timer will only return after an idle period since the last active
			// operation (read or write).
			return newSessionIdleErr(closeAfterIdle)
		case <-s.activeAtChan:
			// The session is still active, we want to reset the timer. First we have to stop the timer, drain the
			// current value and then reset. It's okay if we lose some time on this operation as we don't need to
			// close an idle session directly on-time.
			if !checkIdleTimer.Stop() {
				<-checkIdleTimer.C
			}
			checkIdleTimer.Reset(closeAfterIdle)
		}
	}
}
