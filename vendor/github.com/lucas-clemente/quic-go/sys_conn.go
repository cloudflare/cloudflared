package quic

import (
	"net"
	"syscall"
	"time"

	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/utils"
)

// OOBCapablePacketConn is a connection that allows the reading of ECN bits from the IP header.
// If the PacketConn passed to Dial or Listen satisfies this interface, quic-go will use it.
// In this case, ReadMsgUDP() will be used instead of ReadFrom() to read packets.
type OOBCapablePacketConn interface {
	net.PacketConn
	SyscallConn() (syscall.RawConn, error)
	ReadMsgUDP(b, oob []byte) (n, oobn, flags int, addr *net.UDPAddr, err error)
	WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (n, oobn int, err error)
}

var _ OOBCapablePacketConn = &net.UDPConn{}

func wrapConn(pc net.PacketConn) (rawConn, error) {
	conn, ok := pc.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if ok {
		rawConn, err := conn.SyscallConn()
		if err != nil {
			return nil, err
		}
		err = setDF(rawConn)
		if err != nil {
			return nil, err
		}
	}
	c, ok := pc.(OOBCapablePacketConn)
	if !ok {
		utils.DefaultLogger.Infof("PacketConn is not a net.UDPConn. Disabling optimizations possible on UDP connections.")
		return &basicConn{PacketConn: pc}, nil
	}
	return newConn(c)
}

// The basicConn is the most trivial implementation of a connection.
// It reads a single packet from the underlying net.PacketConn.
// It is used when
// * the net.PacketConn is not a OOBCapablePacketConn, and
// * when the OS doesn't support OOB.
type basicConn struct {
	net.PacketConn
}

var _ rawConn = &basicConn{}

func (c *basicConn) ReadPacket() (*receivedPacket, error) {
	buffer := getPacketBuffer()
	// The packet size should not exceed protocol.MaxPacketBufferSize bytes
	// If it does, we only read a truncated packet, which will then end up undecryptable
	buffer.Data = buffer.Data[:protocol.MaxPacketBufferSize]
	n, addr, err := c.PacketConn.ReadFrom(buffer.Data)
	if err != nil {
		return nil, err
	}
	return &receivedPacket{
		remoteAddr: addr,
		rcvTime:    time.Now(),
		data:       buffer.Data[:n],
		buffer:     buffer,
	}, nil
}

func (c *basicConn) WritePacket(b []byte, addr net.Addr, _ []byte) (n int, err error) {
	return c.PacketConn.WriteTo(b, addr)
}
