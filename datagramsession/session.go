package datagramsession

import (
	"context"
	"io"

	"github.com/google/uuid"
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
	doneChan  chan struct{}
}

func newSession(id uuid.UUID, transport transport, dstConn io.ReadWriteCloser) *Session {
	return &Session{
		id:        id,
		transport: transport,
		dstConn:   dstConn,
		doneChan:  make(chan struct{}),
	}
}

func (s *Session) Serve(ctx context.Context) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-serveCtx.Done():
		case <-s.doneChan:
		}
		s.dstConn.Close()
	}()
	// QUIC implementation copies data to another buffer before returning https://github.com/lucas-clemente/quic-go/blob/v0.24.0/session.go#L1967-L1975
	// This makes it safe to share readBuffer between iterations
	readBuffer := make([]byte, 1280)
	for {
		// TODO: TUN-5303: origin proxy should determine the buffer size
		n, err := s.dstConn.Read(readBuffer)
		if n > 0 {
			if err := s.transport.SendTo(s.id, readBuffer[:n]); err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
	}
}

func (s *Session) writeToDst(payload []byte) (int, error) {
	return s.dstConn.Write(payload)
}

func (s *Session) close() {
	close(s.doneChan)
}
