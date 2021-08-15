// +build darwin

package quic

import "golang.org/x/sys/unix"

const msgTypeIPTOS = unix.IP_RECVTOS

const (
	ipv4RECVPKTINFO = unix.IP_RECVPKTINFO
	ipv6RECVPKTINFO = 0x3d
)

const (
	msgTypeIPv4PKTINFO = unix.IP_PKTINFO
	msgTypeIPv6PKTINFO = 0x2e
)
