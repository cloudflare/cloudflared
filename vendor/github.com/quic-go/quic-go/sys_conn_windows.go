//go:build windows

package quic

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

func newConn(c OOBCapablePacketConn) (rawConn, error) {
	return &basicConn{PacketConn: c}, nil
}

func inspectReadBuffer(c net.PacketConn) (int, error) {
	conn, ok := c.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return 0, errors.New("doesn't have a SyscallConn")
	}
	rawConn, err := conn.SyscallConn()
	if err != nil {
		return 0, fmt.Errorf("couldn't get syscall.RawConn: %w", err)
	}
	var size int
	var serr error
	if err := rawConn.Control(func(fd uintptr) {
		size, serr = windows.GetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_RCVBUF)
	}); err != nil {
		return 0, err
	}
	return size, serr
}

func (i *packetInfo) OOB() []byte { return nil }
