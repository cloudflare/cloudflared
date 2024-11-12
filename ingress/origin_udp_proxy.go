package ingress

import (
	"fmt"
	"io"
	"net"
	"net/netip"
)

type UDPProxy interface {
	io.ReadWriteCloser
	LocalAddr() net.Addr
}

type udpProxy struct {
	*net.UDPConn
}

func DialUDP(dstIP net.IP, dstPort uint16) (UDPProxy, error) {
	dstAddr := &net.UDPAddr{
		IP:   dstIP,
		Port: int(dstPort),
	}

	// We use nil as local addr to force runtime to find the best suitable local address IP given the destination
	// address as context.
	udpConn, err := net.DialUDP("udp", nil, dstAddr)
	if err != nil {
		return nil, fmt.Errorf("unable to create UDP proxy to origin (%v:%v): %w", dstIP, dstPort, err)
	}

	return &udpProxy{udpConn}, nil
}

func DialUDPAddrPort(dest netip.AddrPort) (*net.UDPConn, error) {
	addr := net.UDPAddrFromAddrPort(dest)

	// We use nil as local addr to force runtime to find the best suitable local address IP given the destination
	// address as context.
	udpConn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("unable to dial udp to origin %s: %w", dest, err)
	}

	return udpConn, nil
}
