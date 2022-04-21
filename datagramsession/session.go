package datagramsession

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	defaultCloseIdleAfter = time.Second * 210
)

func SessionIdleErr(timeout time.Duration) error {
	return fmt.Errorf("session idle for %v", timeout)
}

// Session is a bidirectional pipe of datagrams between transport and dstConn
// Currently the only implementation of transport is quic DatagramMuxer
// Destination can be a connection with origin or with eyeball
// When the destination is origin:
// - Datagrams from edge are read by Manager from the transport. Manager finds the corresponding Session and calls the
//   write method of the Session to send to origin
// - Datagrams from origin are read from conn and SentTo transport. Transport will return them to eyeball
// When the destination is eyeball:
// - Datagrams from eyeball are read from conn and SentTo transport. Transport will send them to cloudflared
// - Datagrams from cloudflared are read by Manager from the transport. Manager finds the corresponding Session and calls the
//   write method of the Session to send to eyeball
type Session struct {
	ID        uuid.UUID
	transport transport
	dstConn   io.ReadWriteCloser
	// activeAtChan is used to communicate the last read/write time
	activeAtChan chan time.Time
	closeChan    chan error
	log          *zerolog.Logger
}

func (s *Session) Serve(ctx context.Context, closeAfterIdle time.Duration) (closedByRemote bool, err error) {
	go func() {
		// QUIC implementation copies data to another buffer before returning https://github.com/lucas-clemente/quic-go/blob/v0.24.0/session.go#L1967-L1975
		// This makes it safe to share readBuffer between iterations
		const maxPacketSize = 1500
		readBuffer := make([]byte, maxPacketSize)
		for {
			if err := s.dstToTransport(readBuffer); err != nil {
				s.closeChan <- err
				return
			}
		}
	}()
	err = s.waitForCloseCondition(ctx, closeAfterIdle)
	if closeSession, ok := err.(*errClosedSession); ok {
		closedByRemote = closeSession.byRemote
	}
	return closedByRemote, err
}

func (s *Session) waitForCloseCondition(ctx context.Context, closeAfterIdle time.Duration) error {
	// Closing dstConn cancels read so dstToTransport routine in Serve() can return
	defer s.dstConn.Close()
	if closeAfterIdle == 0 {
		// provide deafult is caller doesn't specify one
		closeAfterIdle = defaultCloseIdleAfter
	}

	checkIdleFreq := closeAfterIdle / 8
	checkIdleTicker := time.NewTicker(checkIdleFreq)
	defer checkIdleTicker.Stop()

	activeAt := time.Now()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case reason := <-s.closeChan:
			return reason
		// TODO: TUN-5423 evaluate if using atomic is more efficient
		case now := <-checkIdleTicker.C:
			// The session is considered inactive if current time is after (last active time + allowed idle time)
			if now.After(activeAt.Add(closeAfterIdle)) {
				return SessionIdleErr(closeAfterIdle)
			}
		case activeAt = <-s.activeAtChan: // Update last active time
		}
	}
}

func (s *Session) dstToTransport(buffer []byte) error {
	n, err := s.dstConn.Read(buffer)
	s.markActive()
	// https://pkg.go.dev/io#Reader suggests caller should always process n > 0 bytes
	if n > 0 {
		if n <= int(s.transport.MTU()) {
			err = s.transport.SendTo(s.ID, buffer[:n])
		} else {
			// drop packet for now, eventually reply with ICMP for PMTUD
			s.log.Debug().
				Str("session", s.ID.String()).
				Int("len", n).
				Int("mtu", s.transport.MTU()).
				Msg("dropped packet exceeding MTU")
		}
	}
	// Some UDP application might send 0-size payload.
	if err == nil && n == 0 {
		err = s.transport.SendTo(s.ID, []byte{})
	}
	return err
}

func (s *Session) transportToDst(payload []byte) (int, error) {
	s.markActive()
	return s.dstConn.Write(payload)
}

// Sends the last active time to the idle checker loop without blocking. activeAtChan will only be full when there
// are many concurrent read/write. It is fine to lose some precision
func (s *Session) markActive() {
	select {
	case s.activeAtChan <- time.Now():
	default:
	}
}

func (s *Session) close(err *errClosedSession) {
	s.closeChan <- err
}
