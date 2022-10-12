package quic

import (
	"sync"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

// A closedLocalConn is a connection that we closed locally.
// When receiving packets for such a connection, we need to retransmit the packet containing the CONNECTION_CLOSE frame,
// with an exponential backoff.
type closedLocalConn struct {
	conn            sendConn
	connClosePacket []byte

	closeOnce sync.Once
	closeChan chan struct{} // is closed when the connection is closed or destroyed

	receivedPackets chan *receivedPacket
	counter         uint64 // number of packets received

	perspective protocol.Perspective

	logger utils.Logger
}

var _ packetHandler = &closedLocalConn{}

// newClosedLocalConn creates a new closedLocalConn and runs it.
func newClosedLocalConn(
	conn sendConn,
	connClosePacket []byte,
	perspective protocol.Perspective,
	logger utils.Logger,
) packetHandler {
	s := &closedLocalConn{
		conn:            conn,
		connClosePacket: connClosePacket,
		perspective:     perspective,
		logger:          logger,
		closeChan:       make(chan struct{}),
		receivedPackets: make(chan *receivedPacket, 64),
	}
	go s.run()
	return s
}

func (s *closedLocalConn) run() {
	for {
		select {
		case p := <-s.receivedPackets:
			s.handlePacketImpl(p)
		case <-s.closeChan:
			return
		}
	}
}

func (s *closedLocalConn) handlePacket(p *receivedPacket) {
	select {
	case s.receivedPackets <- p:
	default:
	}
}

func (s *closedLocalConn) handlePacketImpl(_ *receivedPacket) {
	s.counter++
	// exponential backoff
	// only send a CONNECTION_CLOSE for the 1st, 2nd, 4th, 8th, 16th, ... packet arriving
	for n := s.counter; n > 1; n = n / 2 {
		if n%2 != 0 {
			return
		}
	}
	s.logger.Debugf("Received %d packets after sending CONNECTION_CLOSE. Retransmitting.", s.counter)
	if err := s.conn.Write(s.connClosePacket); err != nil {
		s.logger.Debugf("Error retransmitting CONNECTION_CLOSE: %s", err)
	}
}

func (s *closedLocalConn) shutdown() {
	s.destroy(nil)
}

func (s *closedLocalConn) destroy(error) {
	s.closeOnce.Do(func() {
		close(s.closeChan)
	})
}

func (s *closedLocalConn) getPerspective() protocol.Perspective {
	return s.perspective
}

// A closedRemoteConn is a connection that was closed remotely.
// For such a connection, we might receive reordered packets that were sent before the CONNECTION_CLOSE.
// We can just ignore those packets.
type closedRemoteConn struct {
	perspective protocol.Perspective
}

var _ packetHandler = &closedRemoteConn{}

func newClosedRemoteConn(pers protocol.Perspective) packetHandler {
	return &closedRemoteConn{perspective: pers}
}

func (s *closedRemoteConn) handlePacket(*receivedPacket)         {}
func (s *closedRemoteConn) shutdown()                            {}
func (s *closedRemoteConn) destroy(error)                        {}
func (s *closedRemoteConn) getPerspective() protocol.Perspective { return s.perspective }
