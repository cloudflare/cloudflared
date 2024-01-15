//go:build darwin || linux

package ingress

// This file extracts logic shared by Linux and Darwin implementation if ICMPProxy.

import (
	"fmt"
	"net"
	"net/netip"
	"sync/atomic"

	"github.com/google/gopacket/layers"
	"github.com/rs/zerolog"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
)

// Opens a non-privileged ICMP socket on Linux and Darwin
func newICMPConn(listenIP netip.Addr, zone string) (*icmp.PacketConn, error) {
	if listenIP.Is4() {
		return icmp.ListenPacket("udp4", listenIP.String())
	}
	listenAddr := listenIP.String()
	if zone != "" {
		listenAddr = listenAddr + "%" + zone
	}
	return icmp.ListenPacket("udp6", listenAddr)
}

func netipAddr(addr net.Addr) (netip.Addr, bool) {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	return netip.AddrFromSlice(udpAddr.IP)
}

type flow3Tuple struct {
	srcIP          netip.Addr
	dstIP          netip.Addr
	originalEchoID int
}

// icmpEchoFlow implements the packet.Funnel interface.
type icmpEchoFlow struct {
	*packet.ActivityTracker
	closeCallback  func() error
	closed         *atomic.Bool
	src            netip.Addr
	originConn     *icmp.PacketConn
	responder      *packetResponder
	assignedEchoID int
	originalEchoID int
	// it's up to the user to ensure respEncoder is not used concurrently
	respEncoder *packet.Encoder
}

func newICMPEchoFlow(src netip.Addr, closeCallback func() error, originConn *icmp.PacketConn, responder *packetResponder, assignedEchoID, originalEchoID int, respEncoder *packet.Encoder) *icmpEchoFlow {
	return &icmpEchoFlow{
		ActivityTracker: packet.NewActivityTracker(),
		closeCallback:   closeCallback,
		closed:          &atomic.Bool{},
		src:             src,
		originConn:      originConn,
		responder:       responder,
		assignedEchoID:  assignedEchoID,
		originalEchoID:  originalEchoID,
		respEncoder:     respEncoder,
	}
}

func (ief *icmpEchoFlow) Equal(other packet.Funnel) bool {
	otherICMPFlow, ok := other.(*icmpEchoFlow)
	if !ok {
		return false
	}
	if otherICMPFlow.src != ief.src {
		return false
	}
	if otherICMPFlow.originalEchoID != ief.originalEchoID {
		return false
	}
	if otherICMPFlow.assignedEchoID != ief.assignedEchoID {
		return false
	}
	return true
}

func (ief *icmpEchoFlow) Close() error {
	ief.closed.Store(true)
	return ief.closeCallback()
}

func (ief *icmpEchoFlow) IsClosed() bool {
	return ief.closed.Load()
}

// sendToDst rewrites the echo ID to the one assigned to this flow
func (ief *icmpEchoFlow) sendToDst(dst netip.Addr, msg *icmp.Message) error {
	ief.UpdateLastActive()
	originalEcho, err := getICMPEcho(msg)
	if err != nil {
		return err
	}
	sendMsg := icmp.Message{
		Type: msg.Type,
		Code: msg.Code,
		Body: &icmp.Echo{
			ID:   ief.assignedEchoID,
			Seq:  originalEcho.Seq,
			Data: originalEcho.Data,
		},
	}
	// For IPv4, the pseudoHeader is not used because the checksum is always calculated
	var pseudoHeader []byte = nil
	serializedPacket, err := sendMsg.Marshal(pseudoHeader)
	if err != nil {
		return err
	}
	_, err = ief.originConn.WriteTo(serializedPacket, &net.UDPAddr{
		IP: dst.AsSlice(),
	})
	return err
}

// returnToSrc rewrites the echo ID to the original echo ID from the eyeball
func (ief *icmpEchoFlow) returnToSrc(reply *echoReply) error {
	ief.UpdateLastActive()
	reply.echo.ID = ief.originalEchoID
	reply.msg.Body = reply.echo
	pk := packet.ICMP{
		IP: &packet.IP{
			Src:      reply.from,
			Dst:      ief.src,
			Protocol: layers.IPProtocol(reply.msg.Type.Protocol()),
			TTL:      packet.DefaultTTL,
		},
		Message: reply.msg,
	}
	serializedPacket, err := ief.respEncoder.Encode(&pk)
	if err != nil {
		return err
	}
	return ief.responder.returnPacket(serializedPacket)
}

type echoReply struct {
	from netip.Addr
	msg  *icmp.Message
	echo *icmp.Echo
}

func parseReply(from net.Addr, rawMsg []byte) (*echoReply, error) {
	fromAddr, ok := netipAddr(from)
	if !ok {
		return nil, fmt.Errorf("cannot convert %s to netip.Addr", from)
	}
	proto := layers.IPProtocolICMPv4
	if fromAddr.Is6() {
		proto = layers.IPProtocolICMPv6
	}
	msg, err := icmp.ParseMessage(int(proto), rawMsg)
	if err != nil {
		return nil, err
	}
	echo, err := getICMPEcho(msg)
	if err != nil {
		return nil, err
	}
	return &echoReply{
		from: fromAddr,
		msg:  msg,
		echo: echo,
	}, nil
}

func toICMPEchoFlow(funnel packet.Funnel) (*icmpEchoFlow, error) {
	icmpFlow, ok := funnel.(*icmpEchoFlow)
	if !ok {
		return nil, fmt.Errorf("%v is not *ICMPEchoFunnel", funnel)
	}
	return icmpFlow, nil
}

func createShouldReplaceFunnelFunc(logger *zerolog.Logger, muxer muxer, pk *packet.ICMP, originalEchoID int) func(packet.Funnel) bool {
	return func(existing packet.Funnel) bool {
		existingFlow, err := toICMPEchoFlow(existing)
		if err != nil {
			logger.Err(err).
				Str("src", pk.Src.String()).
				Str("dst", pk.Dst.String()).
				Int("originalEchoID", originalEchoID).
				Msg("Funnel of wrong type found")
			return true
		}
		// Each quic connection should have a unique muxer.
		// If the existing flow has a different muxer, there's a new quic connection where return packets should be
		// routed. Otherwise, return packets will be send to the first observed incoming connection, rather than the
		// most recently observed connection.
		if existingFlow.responder.datagramMuxer != muxer {
			logger.Debug().
				Str("src", pk.Src.String()).
				Str("dst", pk.Dst.String()).
				Int("originalEchoID", originalEchoID).
				Msg("Replacing funnel with new responder")
			return true
		}
		return false
	}
}
