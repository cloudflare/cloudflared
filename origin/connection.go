package origin

import (
	"net"
)

// persistentTCPConn is a wrapper around net.Conn that is noop when Close is called
type persistentConn struct {
	net.Conn
}

func (pc *persistentConn) Close() error {
	return nil
}
