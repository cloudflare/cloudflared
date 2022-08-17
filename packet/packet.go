package packet

import (
	"fmt"
	"net/netip"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/icmp"
)

const (
	defaultTTL    uint8 = 64
	ipv4HeaderLen       = 20
	ipv6HeaderLen       = 40
)

// Packet represents an IP packet or a packet that is encapsulated by IP
type Packet interface {
	// IPLayer returns the IP of the packet
	IPLayer() *IP
	// EncodeLayers returns the layers that make up this packet. They can be passed to an Encoder to serialize into RawPacket
	EncodeLayers() ([]gopacket.SerializableLayer, error)
}

// IP represents a generic IP packet. It can be embedded in more specific IP protocols
type IP struct {
	Src      netip.Addr
	Dst      netip.Addr
	Protocol layers.IPProtocol
}

func newIPv4(ipLayer *layers.IPv4) (*IP, error) {
	src, ok := netip.AddrFromSlice(ipLayer.SrcIP)
	if !ok {
		return nil, fmt.Errorf("cannot convert source IP %s to netip.Addr", ipLayer.SrcIP)
	}
	dst, ok := netip.AddrFromSlice(ipLayer.DstIP)
	if !ok {
		return nil, fmt.Errorf("cannot convert source IP %s to netip.Addr", ipLayer.DstIP)
	}
	return &IP{
		Src:      src,
		Dst:      dst,
		Protocol: ipLayer.Protocol,
	}, nil
}

func newIPv6(ipLayer *layers.IPv6) (*IP, error) {
	src, ok := netip.AddrFromSlice(ipLayer.SrcIP)
	if !ok {
		return nil, fmt.Errorf("cannot convert source IP %s to netip.Addr", ipLayer.SrcIP)
	}
	dst, ok := netip.AddrFromSlice(ipLayer.DstIP)
	if !ok {
		return nil, fmt.Errorf("cannot convert source IP %s to netip.Addr", ipLayer.DstIP)
	}
	return &IP{
		Src:      src,
		Dst:      dst,
		Protocol: ipLayer.NextHeader,
	}, nil
}

func (ip *IP) IPLayer() *IP {
	return ip
}

func (ip *IP) isIPv4() bool {
	return ip.Src.Is4()
}

func (ip *IP) EncodeLayers() ([]gopacket.SerializableLayer, error) {
	if ip.isIPv4() {
		return []gopacket.SerializableLayer{
			&layers.IPv4{
				Version:  4,
				SrcIP:    ip.Src.AsSlice(),
				DstIP:    ip.Dst.AsSlice(),
				Protocol: layers.IPProtocol(ip.Protocol),
				TTL:      defaultTTL,
			},
		}, nil
	} else {
		return []gopacket.SerializableLayer{
			&layers.IPv6{
				Version:    6,
				SrcIP:      ip.Src.AsSlice(),
				DstIP:      ip.Dst.AsSlice(),
				NextHeader: layers.IPProtocol(ip.Protocol),
				HopLimit:   defaultTTL,
			},
		}, nil
	}
}

// ICMP represents is an IP packet + ICMP message
type ICMP struct {
	*IP
	*icmp.Message
}

func (i *ICMP) EncodeLayers() ([]gopacket.SerializableLayer, error) {
	ipLayers, err := i.IP.EncodeLayers()
	if err != nil {
		return nil, err
	}

	msg, err := i.Marshal(nil)
	if err != nil {
		return nil, err
	}
	icmpLayer := gopacket.Payload(msg)
	return append(ipLayers, icmpLayer), nil
}
