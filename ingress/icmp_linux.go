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

func newICMPProxy(listenIP netip.Addr, logger *zerolog.Logger, idleTimeout time.Duration) (ICMPProxy, error) {
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
	funnelID := srcIPFunnelID(pk.Src)
	funnel, exists := ip.srcFunnelTracker.Get(funnelID)
	if !exists {
		originalEcho, err := getICMPEcho(pk.Message)
		if err != nil {
			return err
		}
		conn, err := newICMPConn(ip.listenIP)
		if err != nil {
			return errors.Wrap(err, "failed to open ICMP socket")
		}
		localUDPAddr, ok := conn.LocalAddr().(*net.UDPAddr)
		if !ok {
			return fmt.Errorf("ICMP listener address %s is not net.UDPAddr", conn.LocalAddr())
		}
		originSender := originSender{conn: conn}
		echoID := localUDPAddr.Port
		icmpFlow := newICMPEchoFlow(pk.Src, &originSender, responder, echoID, originalEcho.ID, packet.NewEncoder())
		if replaced := ip.srcFunnelTracker.Register(funnelID, icmpFlow); replaced {
			ip.logger.Info().Str("src", pk.Src.String()).Msg("Replaced funnel")
		}
		if err := icmpFlow.sendToDst(pk.Dst, pk.Message); err != nil {
			return errors.Wrap(err, "failed to send ICMP echo request")
		}
		go func() {
			defer ip.srcFunnelTracker.Unregister(funnelID, icmpFlow)
			if err := ip.listenResponse(icmpFlow, conn); err != nil {
				ip.logger.Err(err).
					Str("funnelID", funnelID.String()).
					Int("echoID", echoID).
					Msg("Failed to listen for ICMP echo response")
			}
		}()
		return nil
	}
	icmpFlow, err := toICMPEchoFlow(funnel)
	if err != nil {
		return err
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
		n, src, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}

		if err := ip.handleResponse(flow, src, buf[:n]); err != nil {
			ip.logger.Err(err).Str("dst", src.String()).Msg("Failed to handle ICMP response")
			continue
		}
	}
}

func (ip *icmpProxy) handleResponse(flow *icmpEchoFlow, from net.Addr, rawMsg []byte) error {
	reply, err := parseReply(from, rawMsg)
	if err != nil {
		return err
	}
	return flow.returnToSrc(reply)
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

type srcIPFunnelID netip.Addr

func (sifd srcIPFunnelID) Type() string {
	return "srcIP"
}

func (sifd srcIPFunnelID) String() string {
	return netip.Addr(sifd).String()
}
