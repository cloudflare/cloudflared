package packet

import (
	"encoding/binary"
	"fmt"
	"net/netip"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	ipv4MinHeaderLen = 20
	ipv6HeaderLen    = 40
	ipv4MinMTU       = 576
	ipv6MinMTU       = 1280
	icmpHeaderLen    = 8
	// https://www.rfc-editor.org/rfc/rfc792 and https://datatracker.ietf.org/doc/html/rfc4443#section-3.3 define 2 codes.
	// 0 = ttl exceed in transit, 1 = fragment reassembly time exceeded
	icmpTTLExceedInTransitCode       = 0
	DefaultTTL                 uint8 = 255
	pseudoHeaderLen                  = 40
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
	TTL      uint8
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
		TTL:      ipLayer.TTL,
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
		TTL:      ipLayer.HopLimit,
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
				TTL:      ip.TTL,
			},
		}, nil
	} else {
		return []gopacket.SerializableLayer{
			&layers.IPv6{
				Version:    6,
				SrcIP:      ip.Src.AsSlice(),
				DstIP:      ip.Dst.AsSlice(),
				NextHeader: layers.IPProtocol(ip.Protocol),
				HopLimit:   ip.TTL,
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

	var serializedPsh []byte = nil
	if i.Protocol == layers.IPProtocolICMPv6 {
		psh := &PseudoHeader{
			SrcIP: i.Src.As16(),
			DstIP: i.Dst.As16(),
			// i.Marshal re-calculates the UpperLayerPacketLength
			UpperLayerPacketLength: 0,
			NextHeader:             uint8(i.Protocol),
		}
		serializedPsh = psh.Marshal()
	}
	msg, err := i.Marshal(serializedPsh)
	if err != nil {
		return nil, err
	}
	icmpLayer := gopacket.Payload(msg)
	return append(ipLayers, icmpLayer), nil
}

// https://www.rfc-editor.org/rfc/rfc2460#section-8.1
type PseudoHeader struct {
	SrcIP                  [16]byte
	DstIP                  [16]byte
	UpperLayerPacketLength uint32
	zero                   [3]byte
	NextHeader             uint8
}

func (ph *PseudoHeader) Marshal() []byte {
	buf := make([]byte, pseudoHeaderLen)
	index := 0
	copy(buf, ph.SrcIP[:])
	index += 16
	copy(buf[index:], ph.DstIP[:])
	index += 16
	binary.BigEndian.PutUint32(buf[index:], ph.UpperLayerPacketLength)
	index += 4
	copy(buf[index:], ph.zero[:])
	buf[pseudoHeaderLen-1] = ph.NextHeader
	return buf
}

func NewICMPTTLExceedPacket(originalIP *IP, originalPacket RawPacket, routerIP netip.Addr) *ICMP {
	var (
		protocol layers.IPProtocol
		icmpType icmp.Type
	)
	if originalIP.Dst.Is4() {
		protocol = layers.IPProtocolICMPv4
		icmpType = ipv4.ICMPTypeTimeExceeded
	} else {
		protocol = layers.IPProtocolICMPv6
		icmpType = ipv6.ICMPTypeTimeExceeded
	}

	return &ICMP{
		IP: &IP{
			Src:      routerIP,
			Dst:      originalIP.Src,
			Protocol: protocol,
			TTL:      DefaultTTL,
		},
		Message: &icmp.Message{
			Type: icmpType,
			Code: icmpTTLExceedInTransitCode,
			Body: &icmp.TimeExceeded{
				Data: originalDatagram(originalPacket, originalIP.Dst.Is4()),
			},
		},
	}
}

// originalDatagram returns a slice of the original datagram for ICMP error messages
// https://www.rfc-editor.org/rfc/rfc1812#section-4.3.2.3 suggests to copy as much without exceeding 576 bytes.
// https://datatracker.ietf.org/doc/html/rfc4443#section-3.3 suggests to copy as much without exceeding 1280 bytes
func originalDatagram(originalPacket RawPacket, isIPv4 bool) []byte {
	var upperBound int
	if isIPv4 {
		upperBound = ipv4MinMTU - ipv4MinHeaderLen - icmpHeaderLen
		if upperBound > len(originalPacket.Data) {
			upperBound = len(originalPacket.Data)
		}
	} else {
		upperBound = ipv6MinMTU - ipv6HeaderLen - icmpHeaderLen
		if upperBound > len(originalPacket.Data) {
			upperBound = len(originalPacket.Data)
		}
	}
	return originalPacket.Data[:upperBound]
}
