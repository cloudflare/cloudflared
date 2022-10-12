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
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
)

const (
	// https://lwn.net/Articles/550551/ IPv4 and IPv6 share the same path
	pingGroupPath = "/proc/sys/net/ipv4/ping_group_range"
)

var (
	findGroupIDRegex = regexp.MustCompile(`\d+`)
)

type icmpProxy struct {
	srcFunnelTracker *packet.FunnelTracker
	listenIP         netip.Addr
	ipv6Zone         string
	logger           *zerolog.Logger
	idleTimeout      time.Duration
}

func newICMPProxy(listenIP netip.Addr, zone string, logger *zerolog.Logger, idleTimeout time.Duration) (*icmpProxy, error) {
	if err := testPermission(listenIP, zone, logger); err != nil {
		return nil, err
	}
	return &icmpProxy{
		srcFunnelTracker: packet.NewFunnelTracker(),
		listenIP:         listenIP,
		ipv6Zone:         zone,
		logger:           logger,
		idleTimeout:      idleTimeout,
	}, nil
}

func testPermission(listenIP netip.Addr, zone string, logger *zerolog.Logger) error {
	// Opens a non-privileged ICMP socket. On Linux the group ID of the process needs to be in ping_group_range
	// Only check ping_group_range once for IPv4
	if listenIP.Is4() {
		if err := checkInPingGroup(); err != nil {
			logger.Warn().Err(err).Msgf("The user running cloudflared process has a GID (group ID) that is not within ping_group_range. You might need to add that user to a group within that range, or instead update the range to encompass a group the user is already in by modifying %s. Otherwise cloudflared will not be able to ping this network", pingGroupPath)
			return err
		}
	}
	conn, err := newICMPConn(listenIP, zone)
	if err != nil {
		return err
	}
	// This conn is only to test if cloudflared has permission to open this type of socket
	conn.Close()
	return nil
}

func checkInPingGroup() error {
	file, err := os.ReadFile(pingGroupPath)
	if err != nil {
		return err
	}
	groupID := os.Getgid()
	// Example content: 999	   59999
	found := findGroupIDRegex.FindAll(file, 2)
	if len(found) == 2 {
		groupMin, err := strconv.ParseInt(string(found[0]), 10, 32)
		if err != nil {
			return errors.Wrapf(err, "failed to determine minimum ping group ID")
		}
		groupMax, err := strconv.ParseInt(string(found[1]), 10, 32)
		if err != nil {
			return errors.Wrapf(err, "failed to determine minimum ping group ID")
		}
		if groupID < int(groupMin) || groupID > int(groupMax) {
			return fmt.Errorf("Group ID %d is not between ping group %d to %d", groupID, groupMin, groupMax)
		}
		return nil
	}
	return fmt.Errorf("did not find group range in %s", pingGroupPath)
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
		conn, err := newICMPConn(ip.listenIP, ip.ipv6Zone)
		if err != nil {
			return nil, errors.Wrap(err, "failed to open ICMP socket")
		}
		ip.logger.Debug().Msgf("Opened ICMP socket listen on %s", conn.LocalAddr())
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
