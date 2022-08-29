//go:build linux

package ingress

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
)

// The request echo ID is rewritten to the port of the socket. The kernel uses the reply echo ID to demultiplex
// We can open a socket for each source so multiple sources requesting the same destination doesn't collide
type icmpProxy struct {
	srcToFlowTracker *srcToFlowTracker
	listenIP         netip.Addr
	logger           *zerolog.Logger
	shutdownC        chan struct{}
}

func newICMPProxy(listenIP netip.Addr, logger *zerolog.Logger) (ICMPProxy, error) {
	if err := testPermission(listenIP); err != nil {
		return nil, err
	}
	return &icmpProxy{
		srcToFlowTracker: newSrcToConnTracker(),
		listenIP:         listenIP,
		logger:           logger,
		shutdownC:        make(chan struct{}),
	}, nil
}

func testPermission(listenIP netip.Addr) error {
	// Opens a non-privileged ICMP socket. On Linux the group ID of the process needs to be in ping_group_range
	// For more information, see https://man7.org/linux/man-pages/man7/icmp.7.html and https://lwn.net/Articles/422330/
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

func (ip *icmpProxy) Request(pk *packet.ICMP, responder packet.FlowResponder) error {
	if pk == nil {
		return errPacketNil
	}
	echo, err := getICMPEcho(pk)
	if err != nil {
		return err
	}
	return ip.sendICMPEchoRequest(pk, echo, responder)
}

func (ip *icmpProxy) Serve(ctx context.Context) error {
	<-ctx.Done()
	close(ip.shutdownC)
	return ctx.Err()
}

func (ip *icmpProxy) sendICMPEchoRequest(pk *packet.ICMP, echo *icmp.Echo, responder packet.FlowResponder) error {
	icmpFlow, ok := ip.srcToFlowTracker.get(pk.Src)
	if ok {
		return icmpFlow.send(pk)
	}

	conn, err := newICMPConn(ip.listenIP)
	if err != nil {
		return err
	}
	flow := packet.Flow{
		Src:       pk.Src,
		Dst:       pk.Dst,
		Responder: responder,
	}
	icmpFlow = newICMPFlow(conn, &flow, uint16(echo.ID), ip.logger)
	go func() {
		defer ip.srcToFlowTracker.delete(pk.Src)

		if err := icmpFlow.serve(ip.shutdownC, defaultCloseAfterIdle); err != nil {
			ip.logger.Debug().Err(err).Uint16("flowID", icmpFlow.echoID).Msg("flow terminated")
		}
	}()
	ip.srcToFlowTracker.set(pk.Src, icmpFlow)
	return icmpFlow.send(pk)
}

type srcIPFlowID netip.Addr

func (sifd srcIPFlowID) Type() string {
	return "srcIP"
}

func (sifd srcIPFlowID) String() string {
	return netip.Addr(sifd).String()
}

type srcToFlowTracker struct {
	lock sync.RWMutex
	// srcIPToConn tracks source IP to ICMP connection
	srcToFlow map[netip.Addr]*icmpFlow
}

func newSrcToConnTracker() *srcToFlowTracker {
	return &srcToFlowTracker{
		srcToFlow: make(map[netip.Addr]*icmpFlow),
	}
}

func (sft *srcToFlowTracker) get(srcIP netip.Addr) (*icmpFlow, bool) {
	sft.lock.RLock()
	defer sft.lock.RUnlock()

	flow, ok := sft.srcToFlow[srcIP]
	return flow, ok
}

func (sft *srcToFlowTracker) set(srcIP netip.Addr, flow *icmpFlow) {
	sft.lock.Lock()
	defer sft.lock.Unlock()

	sft.srcToFlow[srcIP] = flow
}

func (sft *srcToFlowTracker) delete(srcIP netip.Addr) {
	sft.lock.Lock()
	defer sft.lock.Unlock()

	delete(sft.srcToFlow, srcIP)
}

type icmpFlow struct {
	conn   *icmp.PacketConn
	flow   *packet.Flow
	echoID uint16
	// last active unix time. Unit is seconds
	lastActive int64
	logger     *zerolog.Logger
}

func newICMPFlow(conn *icmp.PacketConn, flow *packet.Flow, echoID uint16, logger *zerolog.Logger) *icmpFlow {
	return &icmpFlow{
		conn:       conn,
		flow:       flow,
		echoID:     echoID,
		lastActive: time.Now().Unix(),
		logger:     logger,
	}
}

func (f *icmpFlow) serve(shutdownC chan struct{}, closeAfterIdle time.Duration) error {
	errC := make(chan error)
	go func() {
		errC <- f.listenResponse()
	}()

	checkIdleTicker := time.NewTicker(closeAfterIdle)
	defer f.conn.Close()
	defer checkIdleTicker.Stop()
	for {
		select {
		case err := <-errC:
			return err
		case <-shutdownC:
			return nil
		case <-checkIdleTicker.C:
			now := time.Now().Unix()
			lastActive := atomic.LoadInt64(&f.lastActive)
			if now > lastActive+int64(closeAfterIdle.Seconds()) {
				return errFlowInactive
			}
		}
	}
}

func (f *icmpFlow) send(pk *packet.ICMP) error {
	f.updateLastActive()

	// For IPv4, the pseudoHeader is not used because the checksum is always calculated
	var pseudoHeader []byte = nil
	serializedMsg, err := pk.Marshal(pseudoHeader)
	if err != nil {
		return errors.Wrap(err, "Failed to encode ICMP message")
	}
	// The address needs to be of type UDPAddr when conn is created without priviledge
	_, err = f.conn.WriteTo(serializedMsg, &net.UDPAddr{
		IP: pk.Dst.AsSlice(),
	})
	return err
}

func (f *icmpFlow) listenResponse() error {
	buf := make([]byte, mtu)
	encoder := packet.NewEncoder()
	for {
		n, src, err := f.conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		f.updateLastActive()

		if err := f.handleResponse(encoder, src, buf[:n]); err != nil {
			f.logger.Err(err).Str("dst", src.String()).Msg("Failed to handle ICMP response")
			continue
		}
	}
}

func (f *icmpFlow) handleResponse(encoder *packet.Encoder, from net.Addr, rawPacket []byte) error {
	// TODO: TUN-6654 Check for IPv6
	msg, err := icmp.ParseMessage(int(layers.IPProtocolICMPv4), rawPacket)
	if err != nil {
		return err
	}

	echo, ok := msg.Body.(*icmp.Echo)
	if !ok {
		return fmt.Errorf("received unexpected icmp type %s from non-privileged ICMP socket", msg.Type)
	}

	addrPort, err := netip.ParseAddrPort(from.String())
	if err != nil {
		return err
	}
	icmpPacket := packet.ICMP{
		IP: &packet.IP{
			Src:      addrPort.Addr(),
			Dst:      f.flow.Src,
			Protocol: layers.IPProtocol(msg.Type.Protocol()),
		},
		Message: &icmp.Message{
			Type: msg.Type,
			Code: msg.Code,
			Body: &icmp.Echo{
				ID:   int(f.echoID),
				Seq:  echo.Seq,
				Data: echo.Data,
			},
		},
	}
	serializedPacket, err := encoder.Encode(&icmpPacket)
	if err != nil {
		return errors.Wrap(err, "Failed to encode ICMP message")
	}
	if err := f.flow.Responder.SendPacket(serializedPacket); err != nil {
		return errors.Wrap(err, "Failed to send packet to the edge")
	}
	return nil
}

func (f *icmpFlow) updateLastActive() {
	atomic.StoreInt64(&f.lastActive, time.Now().Unix())
}
