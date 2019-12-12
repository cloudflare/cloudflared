package clickhouse

import (
	"bufio"
	"crypto/tls"
	"database/sql/driver"
	"net"
	"sync/atomic"
	"time"
)

var tick int32

type openStrategy int8

func (s openStrategy) String() string {
	switch s {
	case connOpenInOrder:
		return "in_order"
	}
	return "random"
}

const (
	connOpenRandom openStrategy = iota + 1
	connOpenInOrder
)

type connOptions struct {
	secure, skipVerify                     bool
	tlsConfig                              *tls.Config
	hosts                                  []string
	connTimeout, readTimeout, writeTimeout time.Duration
	noDelay                                bool
	openStrategy                           openStrategy
	logf                                   func(string, ...interface{})
}

func dial(options connOptions) (*connect, error) {
	var (
		err error
		abs = func(v int) int {
			if v < 0 {
				return -1 * v
			}
			return v
		}
		conn  net.Conn
		ident = abs(int(atomic.AddInt32(&tick, 1)))
	)
	tlsConfig := options.tlsConfig
	if options.secure {
		if tlsConfig == nil {
			tlsConfig = &tls.Config{}
		}
		tlsConfig.InsecureSkipVerify = options.skipVerify
	}
	for i := range options.hosts {
		var num int
		switch options.openStrategy {
		case connOpenInOrder:
			num = i
		case connOpenRandom:
			num = (ident + i) % len(options.hosts)
		}
		switch {
		case options.secure:
			conn, err = tls.DialWithDialer(
				&net.Dialer{
					Timeout: options.connTimeout,
				},
				"tcp",
				options.hosts[num],
				tlsConfig,
			)
		default:
			conn, err = net.DialTimeout("tcp", options.hosts[num], options.connTimeout)
		}
		if err == nil {
			options.logf(
				"[dial] secure=%t, skip_verify=%t, strategy=%s, ident=%d, server=%d -> %s",
				options.secure,
				options.skipVerify,
				options.openStrategy,
				ident,
				num,
				conn.RemoteAddr(),
			)
			if tcp, ok := conn.(*net.TCPConn); ok {
				err = tcp.SetNoDelay(options.noDelay) // Disable or enable the Nagle Algorithm for this tcp socket
				if err != nil {
					return nil, err
				}
			}
			return &connect{
				Conn:         conn,
				logf:         options.logf,
				ident:        ident,
				buffer:       bufio.NewReader(conn),
				readTimeout:  options.readTimeout,
				writeTimeout: options.writeTimeout,
			}, nil
		} else {
			options.logf(
				"[dial err] secure=%t, skip_verify=%t, strategy=%s, ident=%d, addr=%s\n%#v",
				options.secure,
				options.skipVerify,
				options.openStrategy,
				ident,
				options.hosts[num],
				err,
			)
		}
	}
	return nil, err
}

type connect struct {
	net.Conn
	logf                  func(string, ...interface{})
	ident                 int
	buffer                *bufio.Reader
	closed                bool
	readTimeout           time.Duration
	writeTimeout          time.Duration
	lastReadDeadlineTime  time.Time
	lastWriteDeadlineTime time.Time
}

func (conn *connect) Read(b []byte) (int, error) {
	var (
		n      int
		err    error
		total  int
		dstLen = len(b)
	)
	if currentTime := now(); conn.readTimeout != 0 && currentTime.Sub(conn.lastReadDeadlineTime) > (conn.readTimeout>>2) {
		conn.SetReadDeadline(time.Now().Add(conn.readTimeout))
		conn.lastReadDeadlineTime = currentTime
	}
	for total < dstLen {
		if n, err = conn.buffer.Read(b[total:]); err != nil {
			conn.logf("[connect] read error: %v", err)
			conn.Close()
			return n, driver.ErrBadConn
		}
		total += n
	}
	return total, nil
}

func (conn *connect) Write(b []byte) (int, error) {
	var (
		n      int
		err    error
		total  int
		srcLen = len(b)
	)
	if currentTime := now(); conn.writeTimeout != 0 && currentTime.Sub(conn.lastWriteDeadlineTime) > (conn.writeTimeout>>2) {
		conn.SetWriteDeadline(time.Now().Add(conn.writeTimeout))
		conn.lastWriteDeadlineTime = currentTime
	}
	for total < srcLen {
		if n, err = conn.Conn.Write(b[total:]); err != nil {
			conn.logf("[connect] write error: %v", err)
			conn.Close()
			return n, driver.ErrBadConn
		}
		total += n
	}
	return n, nil
}

func (conn *connect) Close() error {
	if !conn.closed {
		conn.closed = true
		return conn.Conn.Close()
	}
	return nil
}
