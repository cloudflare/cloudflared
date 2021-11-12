package connection

import (
	"net"
	"sync"

	"github.com/google/uuid"
)

// TODO: TUN-5422 Unregister session
type udpSessions struct {
	lock     sync.Mutex
	sessions map[uuid.UUID]*net.UDPConn
}

func newUDPSessions() *udpSessions {
	return &udpSessions{
		sessions: make(map[uuid.UUID]*net.UDPConn),
	}
}

func (us *udpSessions) register(id uuid.UUID, dstIP net.IP, dstPort uint16) error {
	us.lock.Lock()
	defer us.lock.Unlock()
	dstAddr := &net.UDPAddr{
		IP:   dstIP,
		Port: int(dstPort),
	}
	conn, err := net.DialUDP("udp", us.localAddr(), dstAddr)
	if err != nil {
		return err
	}
	us.sessions[id] = conn
	return nil
}

func (ud *udpSessions) localAddr() *net.UDPAddr {
	// TODO: Determine the IP to bind to
	return &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: 0,
	}
}
