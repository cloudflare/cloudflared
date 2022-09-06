//go:build darwin || linux

package ingress

// This file extracts logic shared by Linux and Darwin implementation if ICMPProxy.

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/google/gopacket/layers"
	"golang.org/x/net/icmp"

	"github.com/cloudflare/cloudflared/packet"
)

// Opens a non-privileged ICMP socket on Linux and Darwin
func newICMPConn(listenIP netip.Addr) (*icmp.PacketConn, error) {
	network := "udp6"
	if listenIP.Is4() {
		network = "udp4"
	}
	return icmp.ListenPacket(network, listenIP.String())
}

func netipAddr(addr net.Addr) (netip.Addr, bool) {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	return netip.AddrFromSlice(udpAddr.IP)
}

type flowID struct {
	srcIP  netip.Addr
	echoID int
}

func (fi *flowID) Type() string {
	return "srcIP_echoID"
}

func (fi *flowID) String() string {
	return fmt.Sprintf("%s:%d", fi.srcIP, fi.echoID)
}

// icmpEchoFlow implements the packet.Funnel interface.
type icmpEchoFlow struct {
	*packet.RawPacketFunnel
	assignedEchoID int
	originalEchoID int
	// it's up to the user to ensure respEncoder is not used concurrently
	respEncoder *packet.Encoder
}

func newICMPEchoFlow(src netip.Addr, sendPipe, returnPipe packet.FunnelUniPipe, assignedEchoID, originalEchoID int, respEncoder *packet.Encoder) *icmpEchoFlow {
	return &icmpEchoFlow{
		RawPacketFunnel: packet.NewRawPacketFunnel(src, sendPipe, returnPipe),
		assignedEchoID:  assignedEchoID,
		originalEchoID:  originalEchoID,
		respEncoder:     respEncoder,
	}
}

// sendToDst rewrites the echo ID to the one assigned to this flow
func (ief *icmpEchoFlow) sendToDst(dst netip.Addr, msg *icmp.Message) error {
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
	return ief.SendToDst(dst, packet.RawPacket{Data: serializedPacket})
}

// returnToSrc rewrites the echo ID to the original echo ID from the eyeball
func (ief *icmpEchoFlow) returnToSrc(reply *echoReply) error {
	reply.echo.ID = ief.originalEchoID
	reply.msg.Body = reply.echo
	pk := packet.ICMP{
		IP: &packet.IP{
			Src:      reply.from,
			Dst:      ief.Src,
			Protocol: layers.IPProtocol(reply.msg.Type.Protocol()),
		},
		Message: reply.msg,
	}
	serializedPacket, err := ief.respEncoder.Encode(&pk)
	if err != nil {
		return err
	}
	return ief.ReturnToSrc(serializedPacket)
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
