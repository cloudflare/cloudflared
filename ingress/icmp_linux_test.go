//go:build linux

package ingress

import (
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/cloudflare/cloudflared/packet"
)

func TestCloseIdleFlow(t *testing.T) {
	const (
		echoID      = 19234
		idleTimeout = time.Millisecond * 100
	)
	conn, err := newICMPConn(localhostIP)
	require.NoError(t, err)
	flow := packet.Flow{
		Src: netip.MustParseAddr("172.16.0.1"),
	}
	icmpFlow := newICMPFlow(conn, &flow, echoID, &noopLogger)
	shutdownC := make(chan struct{})
	flowErr := make(chan error)
	go func() {
		flowErr <- icmpFlow.serve(shutdownC, idleTimeout)
	}()

	require.Equal(t, errFlowInactive, <-flowErr)
}

func TestCloseConnStopFlow(t *testing.T) {
	const (
		echoID = 19234
	)
	conn, err := newICMPConn(localhostIP)
	require.NoError(t, err)
	flow := packet.Flow{
		Src: netip.MustParseAddr("172.16.0.1"),
	}
	icmpFlow := newICMPFlow(conn, &flow, echoID, &noopLogger)
	shutdownC := make(chan struct{})
	conn.Close()

	err = icmpFlow.serve(shutdownC, defaultCloseAfterIdle)
	require.True(t, errors.Is(err, net.ErrClosed))
}
