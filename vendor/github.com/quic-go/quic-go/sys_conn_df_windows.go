//go:build windows

package quic

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/quic-go/quic-go/internal/utils"
)

const (
	// same for both IPv4 and IPv6 on Windows
	// https://microsoft.github.io/windows-docs-rs/doc/windows/Win32/Networking/WinSock/constant.IP_DONTFRAG.html
	// https://microsoft.github.io/windows-docs-rs/doc/windows/Win32/Networking/WinSock/constant.IPV6_DONTFRAG.html
	IP_DONTFRAGMENT = 14
	IPV6_DONTFRAG   = 14
)

func setDF(rawConn syscall.RawConn) error {
	var errDFIPv4, errDFIPv6 error
	if err := rawConn.Control(func(fd uintptr) {
		errDFIPv4 = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, IP_DONTFRAGMENT, 1)
		errDFIPv6 = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, IPV6_DONTFRAG, 1)
	}); err != nil {
		return err
	}
	switch {
	case errDFIPv4 == nil && errDFIPv6 == nil:
		utils.DefaultLogger.Debugf("Setting DF for IPv4 and IPv6.")
	case errDFIPv4 == nil && errDFIPv6 != nil:
		utils.DefaultLogger.Debugf("Setting DF for IPv4.")
	case errDFIPv4 != nil && errDFIPv6 == nil:
		utils.DefaultLogger.Debugf("Setting DF for IPv6.")
	case errDFIPv4 != nil && errDFIPv6 != nil:
		return errors.New("setting DF failed for both IPv4 and IPv6")
	}
	return nil
}

func isMsgSizeErr(err error) bool {
	// https://docs.microsoft.com/en-us/windows/win32/winsock/windows-sockets-error-codes-2
	return errors.Is(err, windows.WSAEMSGSIZE)
}
