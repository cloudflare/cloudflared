//go:build linux

package ingress

// This file implements ICMPProxy for Linux. Each (source IP, destination IP, echo ID) opens a non-privileged ICMP socket.
// The source IP of the requests are rewritten to the bind IP of the socket and echo ID rewritten to the port number of
// the socket. The kernel ensures the socket only reads replies whose echo ID matches the port number.
// For more information about the socket, see https://man7.org/linux/man-pages/man7/icmp.7.html and https://lwn.net/Articles/422330/

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
)

type icmpProxy struct {
	srcFunnelTracker *packet.FunnelTracker
	listenIP         netip.Addr
	logger           *zerolog.Logger
	idleTimeout      time.Duration
}

func newICMPProxy(listenIP netip.Addr, logger *zerolog.Logger, idleTimeout time.Duration) (*icmpProxy, error) {
	if err := testPermission(listenIP); err != nil {
		return nil, err
	}
	return &icmpProxy{
		srcFunnelTracker: packet.NewFunnelTracker(),
		listenIP:         listenIP,
		logger:           logger,
		idleTimeout:      idleTimeout,
	}, nil
}

func testPermission(listenIP netip.Addr) error {
	// Opens a non-privileged ICMP socket. On Linux the group ID of the process needs to be in ping_group_range
	conn, err := newICMPConn(listenIP)
	if err != nil {
		// TODO: TUN-6715 check if cloudflared is in ping_group_range if the check failed. If not log instruction to
		// change the group ID
		return err
	}
	// This conn is only to test if cloudflared has permission to open this type of socket
	conn.Close()
	return nil
}

func (ip *icmpProxy) Request(pk *packet.ICMP, responder packet.FunnelUniPipe) error {
	if pk == nil {
		return errPacketNil
	}
	originalEcho, err := getICMPEcho(pk.Message)
	if err != nil {
		return err
	}
	newConnChan := make(chan *icmp.PacketConn, 1)
	newFunnelFunc := func() (packet.Funnel, error) {
		conn, err := newICMPConn(ip.listenIP)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open ICMP socket")
		}
		newConnChan <- conn
		localUDPAddr, ok := conn.LocalAddr().(*net.UDPAddr)
		if !ok {
			return nil, fmt.Errorf("ICMP listener address %s is not net.UDPAddr", conn.LocalAddr())
		}
		originSender := originSender{conn: conn}
		echoID := localUDPAddr.Port
		icmpFlow := newICMPEchoFlow(pk.Src, &originSender, responder, echoID, originalEcho.ID, packet.NewEncoder())
		return icmpFlow, nil
	}
	funnelID := flow3Tuple{
		srcIP:          pk.Src,
		dstIP:          pk.Dst,
		originalEchoID: originalEcho.ID,
	}
	funnel, isNew, err := ip.srcFunnelTracker.GetOrRegister(funnelID, newFunnelFunc)
	if err != nil {
		return err
	}
	icmpFlow, err := toICMPEchoFlow(funnel)
	if err != nil {
		return err
	}
	if isNew {
		ip.logger.Debug().
			Str("src", pk.Src.String()).
			Str("dst", pk.Dst.String()).
			Int("originalEchoID", originalEcho.ID).
			Msg("New flow")
		conn := <-newConnChan
		go func() {
			defer ip.srcFunnelTracker.Unregister(funnelID, icmpFlow)
			if err := ip.listenResponse(icmpFlow, conn); err != nil {
				ip.logger.Debug().Err(err).
					Str("src", pk.Src.String()).
					Str("dst", pk.Dst.String()).
					Int("originalEchoID", originalEcho.ID).
					Msg("Failed to listen for ICMP echo response")
			}
		}()
	}
	if err := icmpFlow.sendToDst(pk.Dst, pk.Message); err != nil {
		return errors.Wrap(err, "failed to send ICMP echo request")
	}
	return nil
}

func (ip *icmpProxy) Serve(ctx context.Context) error {
	ip.srcFunnelTracker.ScheduleCleanup(ctx, ip.idleTimeout)
	return ctx.Err()
}

func (ip *icmpProxy) listenResponse(flow *icmpEchoFlow, conn *icmp.PacketConn) error {
	buf := make([]byte, mtu)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		reply, err := parseReply(from, buf[:n])
		if err != nil {
			ip.logger.Error().Err(err).Str("dst", from.String()).Msg("Failed to parse ICMP reply")
			continue
		}
		if !isEchoReply(reply.msg) {
			ip.logger.Debug().Str("dst", from.String()).Msgf("Drop ICMP %s from reply", reply.msg.Type)
			continue
		}
		if err := flow.returnToSrc(reply); err != nil {
			ip.logger.Err(err).Str("dst", from.String()).Msg("Failed to send ICMP reply")
			continue
		}
	}
}

// originSender wraps icmp.PacketConn to implement packet.FunnelUniPipe interface
type originSender struct {
	conn *icmp.PacketConn
}

func (os *originSender) SendPacket(dst netip.Addr, pk packet.RawPacket) error {
	_, err := os.conn.WriteTo(pk.Data, &net.UDPAddr{
		IP: dst.AsSlice(),
	})
	return err
}

func (os *originSender) Close() error {
	return os.conn.Close()
}

// Only linux uses flow3Tuple as FunnelID
func (ft flow3Tuple) Type() string {
	return "srcIP_dstIP_echoID"
}

func (ft flow3Tuple) String() string {
	return fmt.Sprintf("%s:%s:%d", ft.srcIP, ft.dstIP, ft.originalEchoID)
}
