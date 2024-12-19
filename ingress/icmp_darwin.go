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
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
	"github.com/cloudflare/cloudflared/tracing"
)

type icmpProxy struct {
	srcFunnelTracker *packet.FunnelTracker
	echoIDTracker    *echoIDTracker
	conn             *icmp.PacketConn
	logger           *zerolog.Logger
	idleTimeout      time.Duration
}

// echoIDTracker tracks which ID has been assigned. It first loops through assignment from lastAssignment to then end,
// then from the beginning to lastAssignment.
// ICMP echo are short lived. By the time an ID is revisited, it should have been released.
type echoIDTracker struct {
	lock sync.Mutex
	// maps the source IP, destination IP and original echo ID to a unique echo ID obtained from assignment
	mapping map[flow3Tuple]uint16
	// assignment tracks if an ID is assigned using index as the ID
	// The size of the array is math.MaxUint16 because echo ID is 2 bytes
	assignment [math.MaxUint16]bool
	// nextAssignment is the next number to check for assigment
	nextAssignment uint16
}

func newEchoIDTracker() *echoIDTracker {
	return &echoIDTracker{
		mapping: make(map[flow3Tuple]uint16),
	}
}

// Get assignment or assign a new ID.
func (eit *echoIDTracker) getOrAssign(key flow3Tuple) (id uint16, success bool) {
	eit.lock.Lock()
	defer eit.lock.Unlock()
	id, exists := eit.mapping[key]
	if exists {
		return id, true
	}

	if eit.nextAssignment == math.MaxUint16 {
		eit.nextAssignment = 0
	}

	for i, assigned := range eit.assignment[eit.nextAssignment:] {
		if !assigned {
			echoID := uint16(i) + eit.nextAssignment
			eit.set(key, echoID)
			return echoID, true
		}
	}
	for i, assigned := range eit.assignment[0:eit.nextAssignment] {
		if !assigned {
			echoID := uint16(i)
			eit.set(key, echoID)
			return echoID, true
		}
	}
	return 0, false
}

// Caller should hold the lock
func (eit *echoIDTracker) set(key flow3Tuple, assignedEchoID uint16) {
	eit.assignment[assignedEchoID] = true
	eit.mapping[key] = assignedEchoID
	eit.nextAssignment = assignedEchoID + 1
}

func (eit *echoIDTracker) release(key flow3Tuple, assigned uint16) bool {
	eit.lock.Lock()
	defer eit.lock.Unlock()

	currentEchoID, exists := eit.mapping[key]
	if exists && assigned == currentEchoID {
		delete(eit.mapping, key)
		eit.assignment[assigned] = false
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

func newICMPProxy(listenIP netip.Addr, logger *zerolog.Logger, idleTimeout time.Duration) (*icmpProxy, error) {
	conn, err := newICMPConn(listenIP)
	if err != nil {
		return nil, err
	}
	logger.Info().Msgf("Created ICMP proxy listening on %s", conn.LocalAddr())
	return &icmpProxy{
		srcFunnelTracker: packet.NewFunnelTracker(),
		echoIDTracker:    newEchoIDTracker(),
		conn:             conn,
		logger:           logger,
		idleTimeout:      idleTimeout,
	}, nil
}

func (ip *icmpProxy) Request(ctx context.Context, pk *packet.ICMP, responder ICMPResponder) error {
	_, span := responder.RequestSpan(ctx, pk)
	defer responder.ExportSpan()

	originalEcho, err := getICMPEcho(pk.Message)
	if err != nil {
		tracing.EndWithErrorStatus(span, err)
		return err
	}
	observeICMPRequest(ip.logger, span, pk.Src.String(), pk.Dst.String(), originalEcho.ID, originalEcho.Seq)

	echoIDTrackerKey := flow3Tuple{
		srcIP:          pk.Src,
		dstIP:          pk.Dst,
		originalEchoID: originalEcho.ID,
	}
	assignedEchoID, success := ip.echoIDTracker.getOrAssign(echoIDTrackerKey)
	if !success {
		err := fmt.Errorf("failed to assign unique echo ID")
		tracing.EndWithErrorStatus(span, err)
		return err
	}
	span.SetAttributes(attribute.Int("assignedEchoID", int(assignedEchoID)))

	shouldReplaceFunnelFunc := createShouldReplaceFunnelFunc(ip.logger, responder, pk, originalEcho.ID)
	newFunnelFunc := func() (packet.Funnel, error) {
		originalEcho, err := getICMPEcho(pk.Message)
		if err != nil {
			return nil, err
		}
		closeCallback := func() error {
			ip.echoIDTracker.release(echoIDTrackerKey, assignedEchoID)
			return nil
		}
		icmpFlow := newICMPEchoFlow(pk.Src, closeCallback, ip.conn, responder, int(assignedEchoID), originalEcho.ID)
		return icmpFlow, nil
	}
	funnelID := echoFunnelID(assignedEchoID)
	funnel, isNew, err := ip.srcFunnelTracker.GetOrRegister(funnelID, shouldReplaceFunnelFunc, newFunnelFunc)
	if err != nil {
		tracing.EndWithErrorStatus(span, err)
		return err
	}
	if isNew {
		span.SetAttributes(attribute.Bool("newFlow", true))
		ip.logger.Debug().
			Str("src", pk.Src.String()).
			Str("dst", pk.Dst.String()).
			Int("originalEchoID", originalEcho.ID).
			Int("assignedEchoID", int(assignedEchoID)).
			Msg("New flow")
	}
	icmpFlow, err := toICMPEchoFlow(funnel)
	if err != nil {
		tracing.EndWithErrorStatus(span, err)
		return err
	}

	err = icmpFlow.sendToDst(pk.Dst, pk.Message)
	if err != nil {
		tracing.EndWithErrorStatus(span, err)
		return err
	}
	tracing.End(span)
	return nil
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
	icmpDecoder := packet.NewICMPDecoder()
	for {
		n, from, err := ip.conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		reply, err := parseReply(from, buf[:n])
		if err != nil {
			ip.logger.Debug().Err(err).Str("dst", from.String()).Msg("Failed to parse ICMP reply, continue to parse as full packet")
			// In unit test, we found out when the listener listens on 0.0.0.0, the socket reads the full packet after
			// the second reply
			if err := ip.handleFullPacket(ctx, icmpDecoder, buf[:n]); err != nil {
				ip.logger.Debug().Err(err).Str("dst", from.String()).Msg("Failed to parse ICMP reply as full packet")
			}
			continue
		}
		if !isEchoReply(reply.msg) {
			ip.logger.Debug().Str("dst", from.String()).Msgf("Drop ICMP %s from reply", reply.msg.Type)
			continue
		}
		if err := ip.sendReply(ctx, reply); err != nil {
			ip.logger.Debug().Err(err).Str("dst", from.String()).Msg("Failed to send ICMP reply")
			continue
		}
	}
}

func (ip *icmpProxy) handleFullPacket(ctx context.Context, decoder *packet.ICMPDecoder, rawPacket []byte) error {
	icmpPacket, err := decoder.Decode(packet.RawPacket{Data: rawPacket})
	if err != nil {
		return err
	}
	echo, err := getICMPEcho(icmpPacket.Message)
	if err != nil {
		return err
	}
	reply := echoReply{
		from: icmpPacket.Src,
		msg:  icmpPacket.Message,
		echo: echo,
	}
	if ip.sendReply(ctx, &reply); err != nil {
		return err
	}
	return nil
}

func (ip *icmpProxy) sendReply(ctx context.Context, reply *echoReply) error {
	funnelID := echoFunnelID(reply.echo.ID)
	funnel, ok := ip.srcFunnelTracker.Get(funnelID)
	if !ok {
		return packet.ErrFunnelNotFound
	}
	icmpFlow, err := toICMPEchoFlow(funnel)
	if err != nil {
		return err
	}

	_, span := icmpFlow.responder.ReplySpan(ctx, ip.logger)
	defer icmpFlow.responder.ExportSpan()

	if err := icmpFlow.returnToSrc(reply); err != nil {
		tracing.EndWithErrorStatus(span, err)
		return err
	}
	observeICMPReply(ip.logger, span, reply.from.String(), reply.echo.ID, reply.echo.Seq)
	span.SetAttributes(attribute.Int("originalEchoID", icmpFlow.originalEchoID))
	tracing.End(span)
	return nil
}
