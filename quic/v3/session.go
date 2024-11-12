package v3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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

	logFlowID        = "flowID"
	logPacketSizeKey = "packetSize"
)

// SessionCloseErr indicates that the session's Close method was called.
var SessionCloseErr error = errors.New("flow was closed directly")

// SessionIdleErr is returned when the session was closed because there was no communication
// in either direction over the session for the timeout period.
type SessionIdleErr struct {
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
	io.WriteCloser
	ID() RequestID
	ConnectionID() uint8
	RemoteAddr() net.Addr
	LocalAddr() net.Addr
	ResetIdleTimer()
	Migrate(eyeball DatagramConn, logger *zerolog.Logger)
	// Serve starts the event loop for processing UDP packets
	Serve(ctx context.Context) error
}

type session struct {
	id             RequestID
	closeAfterIdle time.Duration
	origin         io.ReadWriteCloser
	originAddr     net.Addr
	localAddr      net.Addr
	eyeball        atomic.Pointer[DatagramConn]
	// activeAtChan is used to communicate the last read/write time
	activeAtChan chan time.Time
	closeChan    chan error
	metrics      Metrics
	log          *zerolog.Logger
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
	session := &session{
		id:             id,
		closeAfterIdle: closeAfterIdle,
		origin:         origin,
		originAddr:     originAddr,
		localAddr:      localAddr,
		eyeball:        atomic.Pointer[DatagramConn]{},
		// activeAtChan has low capacity. It can be full when there are many concurrent read/write. markActive() will
		// drop instead of blocking because last active time only needs to be an approximation
		activeAtChan: make(chan time.Time, 1),
		closeChan:    make(chan error, 1),
		metrics:      metrics,
		log:          &logger,
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

func (s *session) Migrate(eyeball DatagramConn, logger *zerolog.Logger) {
	current := *(s.eyeball.Load())
	// Only migrate if the connection ids are different.
	if current.ID() != eyeball.ID() {
		s.eyeball.Store(&eyeball)
		log := logger.With().Str(logFlowID, s.id.String()).Logger()
		s.log = &log
	}
	// The session is already running so we want to restart the idle timeout since no proxied packets have come down yet.
	s.markActive()
	s.metrics.MigrateFlow()
}

func (s *session) Serve(ctx context.Context) error {
	go func() {
		// QUIC implementation copies data to another buffer before returning https://github.com/quic-go/quic-go/blob/v0.24.0/session.go#L1967-L1975
		// This makes it safe to share readBuffer between iterations
		readBuffer := [maxOriginUDPPacketSize + DatagramPayloadHeaderLen]byte{}
		// To perform a zero copy write when passing the datagram to the connection, we prepare the buffer with
		// the required datagram header information. We can reuse this buffer for this session since the header is the
		// same for the each read.
		MarshalPayloadHeaderTo(s.id, readBuffer[:DatagramPayloadHeaderLen])
		for {
			// Read from the origin UDP socket
			n, err := s.origin.Read(readBuffer[DatagramPayloadHeaderLen:])
			if err != nil {
				if errors.Is(err, io.EOF) ||
					errors.Is(err, io.ErrUnexpectedEOF) {
					s.log.Debug().Msgf("flow (origin) connection closed: %v", err)
				}
				s.closeChan <- err
				return
			}
			if n < 0 {
				s.log.Warn().Int(logPacketSizeKey, n).Msg("flow (origin) packet read was negative and was dropped")
				continue
			}
			if n > maxDatagramPayloadLen {
				s.metrics.PayloadTooLarge()
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
				s.closeChan <- err
				return
			}
			// Mark the session as active since we proxied a valid packet from the origin.
			s.markActive()
		}
	}()
	return s.waitForCloseCondition(ctx, s.closeAfterIdle)
}

func (s *session) Write(payload []byte) (n int, err error) {
	n, err = s.origin.Write(payload)
	if err != nil {
		s.log.Err(err).Msg("failed to write payload to flow (remote)")
		return n, err
	}
	// Write must return a non-nil error if it returns n < len(p). https://pkg.go.dev/io#Writer
	if n < len(payload) {
		s.log.Err(io.ErrShortWrite).Msg("failed to write the full payload to flow (remote)")
		return n, io.ErrShortWrite
	}
	// Mark the session as active since we proxied a packet to the origin.
	s.markActive()
	return n, err
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
	return sync.OnceValue(func() error {
		// We don't want to block on sending to the close channel if it is already full
		select {
		case s.closeChan <- SessionCloseErr:
		default:
		}
		return s.origin.Close()
	})()
}

func (s *session) waitForCloseCondition(ctx context.Context, closeAfterIdle time.Duration) error {
	// Closing the session at the end cancels read so Serve() can return
	defer s.Close()
	if closeAfterIdle == 0 {
		// provide deafult is caller doesn't specify one
		closeAfterIdle = defaultCloseIdleAfter
	}

	checkIdleTimer := time.NewTimer(closeAfterIdle)
	defer checkIdleTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case reason := <-s.closeChan:
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
