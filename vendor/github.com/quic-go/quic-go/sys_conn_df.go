//go:build !linux && !windows

package quic

import "syscall"

func setDF(rawConn syscall.RawConn) error {
	// no-op on unsupported platforms
	return nil
}

func isMsgSizeErr(err error) bool {
	// to be implemented for more specific platforms
	return false
}
