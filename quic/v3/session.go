package v3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
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
)

// SessionCloseErr indicates that the session's Close method was called.
var SessionCloseErr error = errors.New("session was closed")

// SessionIdleErr is returned when the session was closed because there was no communication
// in either direction over the session for the timeout period.
type SessionIdleErr struct {
	timeout time.Duration
}

func (e SessionIdleErr) Error() string {
	return fmt.Sprintf("session idle for %v", e.timeout)
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
	// Serve starts the event loop for processing UDP packets
	Serve(ctx context.Context) error
}

type session struct {
	id             RequestID
	closeAfterIdle time.Duration
	origin         io.ReadWriteCloser
	eyeball        DatagramWriter
	// activeAtChan is used to communicate the last read/write time
	activeAtChan chan time.Time
	closeChan    chan error
	log          *zerolog.Logger
}

func NewSession(id RequestID, closeAfterIdle time.Duration, origin io.ReadWriteCloser, eyeball DatagramWriter, log *zerolog.Logger) Session {
	return &session{
		id:             id,
		closeAfterIdle: closeAfterIdle,
		origin:         origin,
		eyeball:        eyeball,
		// activeAtChan has low capacity. It can be full when there are many concurrent read/write. markActive() will
		// drop instead of blocking because last active time only needs to be an approximation
		activeAtChan: make(chan time.Time, 1),
		closeChan:    make(chan error, 1),
		log:          log,
	}
}

func (s *session) ID() RequestID {
	return s.id
}

func (s *session) Serve(ctx context.Context) error {
	go func() {
		// QUIC implementation copies data to another buffer before returning https://github.com/quic-go/quic-go/blob/v0.24.0/session.go#L1967-L1975
		// This makes it safe to share readBuffer between iterations
		readBuffer := [maxOriginUDPPacketSize + datagramPayloadHeaderLen]byte{}
		// To perform a zero copy write when passing the datagram to the connection, we prepare the buffer with
		// the required datagram header information. We can reuse this buffer for this session since the header is the
		// same for the each read.
		MarshalPayloadHeaderTo(s.id, readBuffer[:datagramPayloadHeaderLen])
		for {
			// Read from the origin UDP socket
			n, err := s.origin.Read(readBuffer[datagramPayloadHeaderLen:])
			if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				s.log.Debug().Msg("Session (origin) connection closed")
			}
			if err != nil {
				s.closeChan <- err
				return
			}
			if n < 0 {
				s.log.Warn().Int("packetSize", n).Msg("Session (origin) packet read was negative and was dropped")
				continue
			}
			if n > maxDatagramPayloadLen {
				s.log.Error().Int("packetSize", n).Msg("Session (origin) packet read was too large and was dropped")
				continue
			}
			// Sending a packet to the session does block on the [quic.Connection], however, this is okay because it
			// will cause back-pressure to the kernel buffer if the writes are not fast enough to the edge.
			err = s.eyeball.SendUDPSessionDatagram(readBuffer[:datagramPayloadHeaderLen+n])
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
		s.log.Err(err).Msg("Failed to write payload to session (remote)")
		return n, err
	}
	// Write must return a non-nil error if it returns n < len(p). https://pkg.go.dev/io#Writer
	if n < len(payload) {
		s.log.Err(io.ErrShortWrite).Msg("Failed to write the full payload to session (remote)")
		return n, io.ErrShortWrite
	}
	// Mark the session as active since we proxied a packet to the origin.
	s.markActive()
	return n, err
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
