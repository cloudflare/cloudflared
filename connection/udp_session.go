package connection

import (
	"fmt"
	"net"
	"sync"

	"github.com/google/uuid"
)

// TODO: TUN-5422 Unregister session
const (
	sessionIDLen = len(uuid.UUID{})
)

type udpSessions struct {
	lock     sync.RWMutex
	sessions map[uuid.UUID]*net.UDPConn
	localIP  net.IP
}

func newUDPSessions(localIP net.IP) *udpSessions {
	return &udpSessions{
		sessions: make(map[uuid.UUID]*net.UDPConn),
		localIP:  localIP,
	}
}

func (us *udpSessions) register(id uuid.UUID, dstIP net.IP, dstPort uint16) (*net.UDPConn, error) {
	us.lock.Lock()
	defer us.lock.Unlock()
	dstAddr := &net.UDPAddr{
		IP:   dstIP,
		Port: int(dstPort),
	}
	conn, err := net.DialUDP("udp", us.localAddr(), dstAddr)
	if err != nil {
		return nil, err
	}
	us.sessions[id] = conn
	return conn, nil
}

func (us *udpSessions) unregister(id uuid.UUID) {
	us.lock.Lock()
	defer us.lock.Unlock()
	delete(us.sessions, id)
}

func (us *udpSessions) send(id uuid.UUID, payload []byte) error {
	us.lock.RLock()
	defer us.lock.RUnlock()
	conn, ok := us.sessions[id]
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	_, err := conn.Write(payload)
	return err
}

func (ud *udpSessions) localAddr() *net.UDPAddr {
	// TODO: Determine the IP to bind to

	return &net.UDPAddr{
		IP:   ud.localIP,
		Port: 0,
	}
}

// TODO: TUN-5421 allow user to specify which IP to bind to
func GetLocalIP() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		// Find the IP that is not loop back
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if !ip.IsLoopback() {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("cannot determine IP to bind to")
}
