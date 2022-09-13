package packet

import (
	"context"
	"net"
	"net/netip"

	"github.com/rs/zerolog"
)

var (
	// Source IP in documentation range to return ICMP error messages if we can't determine the IP of this machine
	icmpv4ErrFallbackSrc = netip.MustParseAddr("192.0.2.30")
	icmpv6ErrFallbackSrc = netip.MustParseAddr("2001:db8::")
)

// ICMPRouter sends ICMP messages and listens for their responses
type ICMPRouter interface {
	// Serve starts listening for responses to the requests until context is done
	Serve(ctx context.Context) error
	// Request sends an ICMP message
	Request(pk *ICMP, responder FunnelUniPipe) error
}

// Upstream of raw packets
type Upstream interface {
	// ReceivePacket waits for the next raw packet from upstream
	ReceivePacket(ctx context.Context) (RawPacket, error)
}

type Router struct {
	upstream   Upstream
	returnPipe FunnelUniPipe
	icmpProxy  ICMPRouter
	ipv4Src    netip.Addr
	ipv6Src    netip.Addr
	logger     *zerolog.Logger
}

func NewRouter(upstream Upstream, returnPipe FunnelUniPipe, icmpProxy ICMPRouter, logger *zerolog.Logger) *Router {
	ipv4Src, err := findLocalAddr(net.ParseIP("1.1.1.1"), 53)
	if err != nil {
		logger.Warn().Err(err).Msgf("Failed to determine the IPv4 for this machine. It will use %s as source IP for error messages such as ICMP TTL exceed", icmpv4ErrFallbackSrc)
		ipv4Src = icmpv4ErrFallbackSrc
	}
	ipv6Src, err := findLocalAddr(net.ParseIP("2606:4700:4700::1111"), 53)
	if err != nil {
		logger.Warn().Err(err).Msgf("Failed to determine the IPv6 for this machine. It will use %s as source IP for error messages such as ICMP TTL exceed", icmpv6ErrFallbackSrc)
		ipv6Src = icmpv6ErrFallbackSrc
	}
	return &Router{
		upstream:   upstream,
		returnPipe: returnPipe,
		icmpProxy:  icmpProxy,
		ipv4Src:    ipv4Src,
		ipv6Src:    ipv6Src,
		logger:     logger,
	}
}

func (r *Router) Serve(ctx context.Context) error {
	icmpDecoder := NewICMPDecoder()
	encoder := NewEncoder()
	for {
		rawPacket, err := r.upstream.ReceivePacket(ctx)
		if err != nil {
			return err
		}
		icmpPacket, err := icmpDecoder.Decode(rawPacket)
		if err != nil {
			r.logger.Err(err).Msg("Failed to decode ICMP packet from quic datagram")
			continue
		}

		if icmpPacket.TTL <= 1 {
			if err := r.sendTTLExceedMsg(icmpPacket, rawPacket, encoder); err != nil {
				r.logger.Err(err).Msg("Failed to return ICMP TTL exceed error")
			}
			continue
		}
		icmpPacket.TTL--

		if err := r.icmpProxy.Request(icmpPacket, r.returnPipe); err != nil {
			r.logger.Err(err).
				Str("src", icmpPacket.Src.String()).
				Str("dst", icmpPacket.Dst.String()).
				Interface("type", icmpPacket.Type).
				Msg("Failed to send ICMP packet")
			continue
		}
	}
}

func (r *Router) sendTTLExceedMsg(pk *ICMP, rawPacket RawPacket, encoder *Encoder) error {
	var srcIP netip.Addr
	if pk.Dst.Is4() {
		srcIP = r.ipv4Src
	} else {
		srcIP = r.ipv6Src
	}
	ttlExceedPacket := NewICMPTTLExceedPacket(pk.IP, rawPacket, srcIP)

	encodedTTLExceed, err := encoder.Encode(ttlExceedPacket)
	if err != nil {
		return err
	}
	return r.returnPipe.SendPacket(pk.Src, encodedTTLExceed)
}

// findLocalAddr tries to dial UDP and returns the local address picked by the OS
func findLocalAddr(dst net.IP, port int) (netip.Addr, error) {
	udpConn, err := net.DialUDP("udp", nil, &net.UDPAddr{
		IP:   dst,
		Port: port,
	})
	if err != nil {
		return netip.Addr{}, err
	}
	defer udpConn.Close()
	localAddrPort, err := netip.ParseAddrPort(udpConn.LocalAddr().String())
	if err != nil {
		return netip.Addr{}, err
	}
	localAddr := localAddrPort.Addr()
	return localAddr, nil
}
