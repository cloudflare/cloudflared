//go:build darwin

package ingress

// This file implements ICMPProxy for Darwin. It uses a non-privileged ICMP socket to send echo requests and listen for
// echo replies. The source IP of the requests are rewritten to the bind IP of the socket and the socket reads all
// messages, so we use echo ID to distinguish the replies. Each (source IP, destination IP, echo ID) is assigned a
// unique echo ID.

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
)

// TODO: TUN-6654 Extend support to IPv6
type icmpProxy struct {
	srcFunnelTracker *packet.FunnelTracker
	echoIDTracker    *echoIDTracker
	conn             *icmp.PacketConn
	// Response is handled in one-by-one, so encoder can be shared between funnels
	encoder     *packet.Encoder
	logger      *zerolog.Logger
	idleTimeout time.Duration
}

// echoIDTracker tracks which ID has been assigned. It first loops through assignment from lastAssignment to then end,
// then from the beginning to lastAssignment.
// ICMP echo are short lived. By the time an ID is revisited, it should have been released.
type echoIDTracker struct {
	lock sync.RWMutex
	// maps the source IP to an echo ID obtained from assignment
	srcIPMapping map[netip.Addr]uint16
	// assignment tracks if an ID is assigned using index as the ID
	// The size of the array is math.MaxUint16 because echo ID is 2 bytes
	assignment [math.MaxUint16]bool
	// nextAssignment is the next number to check for assigment
	nextAssignment uint16
}

func newEchoIDTracker() *echoIDTracker {
	return &echoIDTracker{
		srcIPMapping: make(map[netip.Addr]uint16),
	}
}

func (eit *echoIDTracker) get(srcIP netip.Addr) (uint16, bool) {
	eit.lock.RLock()
	defer eit.lock.RUnlock()
	id, ok := eit.srcIPMapping[srcIP]
	return id, ok
}

func (eit *echoIDTracker) assign(srcIP netip.Addr) (uint16, bool) {
	eit.lock.Lock()
	defer eit.lock.Unlock()

	if eit.nextAssignment == math.MaxUint16 {
		eit.nextAssignment = 0
	}

	for i, assigned := range eit.assignment[eit.nextAssignment:] {
		if !assigned {
			echoID := uint16(i) + eit.nextAssignment
			eit.set(srcIP, echoID)
			return echoID, true
		}
	}
	for i, assigned := range eit.assignment[0:eit.nextAssignment] {
		if !assigned {
			echoID := uint16(i)
			eit.set(srcIP, echoID)
			return echoID, true
		}
	}
	return 0, false
}

// Caller should hold the lock
func (eit *echoIDTracker) set(srcIP netip.Addr, echoID uint16) {
	eit.assignment[echoID] = true
	eit.srcIPMapping[srcIP] = echoID
	eit.nextAssignment = echoID + 1
}

func (eit *echoIDTracker) release(srcIP netip.Addr, id uint16) bool {
	eit.lock.Lock()
	defer eit.lock.Unlock()

	currentID, exists := eit.srcIPMapping[srcIP]
	if exists && id == currentID {
		delete(eit.srcIPMapping, srcIP)
		eit.assignment[id] = false
		return true
	}
	return false
}

type echoFunnelID uint16

func (snf echoFunnelID) Type() string {
	return "echoID"
}

func (snf echoFunnelID) String() string {
	return strconv.FormatUint(uint64(snf), 10)
}

func newICMPProxy(listenIP netip.Addr, logger *zerolog.Logger, idleTimeout time.Duration) (ICMPProxy, error) {
	conn, err := newICMPConn(listenIP)
	if err != nil {
		return nil, err
	}
	return &icmpProxy{
		srcFunnelTracker: packet.NewFunnelTracker(),
		echoIDTracker:    newEchoIDTracker(),
		encoder:          packet.NewEncoder(),
		conn:             conn,
		logger:           logger,
		idleTimeout:      idleTimeout,
	}, nil
}

func (ip *icmpProxy) Request(pk *packet.ICMP, responder packet.FunnelUniPipe) error {
	if pk == nil {
		return errPacketNil
	}
	// TODO: TUN-6744 assign unique flow per (src, echo ID)
	echoID, exists := ip.echoIDTracker.get(pk.Src)
	if !exists {
		originalEcho, err := getICMPEcho(pk.Message)
		if err != nil {
			return err
		}
		echoID, exists = ip.echoIDTracker.assign(pk.Src)
		if !exists {
			return fmt.Errorf("failed to assign unique echo ID")
		}
		funnelID := echoFunnelID(echoID)
		originSender := originSender{
			conn:          ip.conn,
			echoIDTracker: ip.echoIDTracker,
			srcIP:         pk.Src,
			echoID:        echoID,
		}
		icmpFlow := newICMPEchoFlow(pk.Src, &originSender, responder, int(echoID), originalEcho.ID, ip.encoder)
		if replaced := ip.srcFunnelTracker.Register(funnelID, icmpFlow); replaced {
			ip.logger.Info().Str("src", pk.Src.String()).Msg("Replaced funnel")
		}
		return icmpFlow.sendToDst(pk.Dst, pk.Message)
	}
	funnel, exists := ip.srcFunnelTracker.Get(echoFunnelID(echoID))
	if !exists {
		return packet.ErrFunnelNotFound
	}
	icmpFlow, err := toICMPEchoFlow(funnel)
	if err != nil {
		return err
	}
	return icmpFlow.sendToDst(pk.Dst, pk.Message)
}

// Serve listens for responses to the requests until context is done
func (ip *icmpProxy) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		ip.conn.Close()
	}()
	go func() {
		ip.srcFunnelTracker.ScheduleCleanup(ctx, ip.idleTimeout)
	}()
	buf := make([]byte, mtu)
	for {
		n, src, err := ip.conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		if err := ip.handleResponse(src, buf[:n]); err != nil {
			ip.logger.Err(err).Str("src", src.String()).Msg("Failed to handle ICMP response")
			continue
		}
	}
}

func (ip *icmpProxy) handleResponse(from net.Addr, rawMsg []byte) error {
	reply, err := parseReply(from, rawMsg)
	if err != nil {
		return err
	}
	funnel, exists := ip.srcFunnelTracker.Get(echoFunnelID(reply.echo.ID))
	if !exists {
		return packet.ErrFunnelNotFound
	}
	icmpFlow, err := toICMPEchoFlow(funnel)
	if err != nil {
		return err
	}
	return icmpFlow.returnToSrc(reply)
}

// originSender wraps icmp.PacketConn to implement packet.FunnelUniPipe interface
type originSender struct {
	conn          *icmp.PacketConn
	echoIDTracker *echoIDTracker
	srcIP         netip.Addr
	echoID        uint16
}

func (os *originSender) SendPacket(dst netip.Addr, pk packet.RawPacket) error {
	_, err := os.conn.WriteTo(pk.Data, &net.UDPAddr{
		IP: dst.AsSlice(),
	})
	return err
}

func (os *originSender) Close() error {
	os.echoIDTracker.release(os.srcIP, os.echoID)
	return nil
}
