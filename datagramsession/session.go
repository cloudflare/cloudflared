package datagramsession

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
)

const (
	defaultCloseIdleAfter = time.Second * 210
)

// Each Session is a bidirectional pipe of datagrams between transport and dstConn
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
	id        uuid.UUID
	transport transport
	dstConn   io.ReadWriteCloser
	// activeAtChan is used to communicate the last read/write time
	activeAtChan chan time.Time
	doneChan     chan struct{}
}

func newSession(id uuid.UUID, transport transport, dstConn io.ReadWriteCloser) *Session {
	return &Session{
		id:        id,
		transport: transport,
		dstConn:   dstConn,
		// activeAtChan has low capacity. It can be full when there are many concurrent read/write. markActive() will
		// drop instead of blocking because last active time only needs to be an approximation
		activeAtChan: make(chan time.Time, 2),
		doneChan:     make(chan struct{}),
	}
}

func (s *Session) Serve(ctx context.Context, closeAfterIdle time.Duration) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.waitForCloseCondition(serveCtx, closeAfterIdle)
	// QUIC implementation copies data to another buffer before returning https://github.com/lucas-clemente/quic-go/blob/v0.24.0/session.go#L1967-L1975
	// This makes it safe to share readBuffer between iterations
	readBuffer := make([]byte, s.transport.MTU())
	for {
		if err := s.dstToTransport(readBuffer); err != nil {
			return err
		}
	}
}

func (s *Session) waitForCloseCondition(ctx context.Context, closeAfterIdle time.Duration) {
	if closeAfterIdle == 0 {
		// provide deafult is caller doesn't specify one
		closeAfterIdle = defaultCloseIdleAfter
	}
	// Closing dstConn cancels read so Serve function can return
	defer s.dstConn.Close()

	checkIdleFreq := closeAfterIdle / 8
	checkIdleTicker := time.NewTicker(checkIdleFreq)
	defer checkIdleTicker.Stop()

	activeAt := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.doneChan:
			return
		// TODO: TUN-5423 evaluate if using atomic is more efficient
		case now := <-checkIdleTicker.C:
			// The session is considered inactive if current time is after (last active time + allowed idle time)
			if now.After(activeAt.Add(closeAfterIdle)) {
				return
			}
		case activeAt = <-s.activeAtChan: // Update last active time
		}
	}
}

func (s *Session) dstToTransport(buffer []byte) error {
	n, err := s.dstConn.Read(buffer)
	s.markActive()
	if n > 0 {
		if err := s.transport.SendTo(s.id, buffer[:n]); err != nil {
			return err
		}
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

func (s *Session) close() {
	close(s.doneChan)
}
