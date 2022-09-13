package packet

import (
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/pkg/errors"
	"golang.org/x/net/icmp"
)

func FindProtocol(p []byte) (layers.IPProtocol, error) {
	version, err := FindIPVersion(p)
	if err != nil {
		return 0, err
	}
	switch version {
	case 4:
		if len(p) < ipv4MinHeaderLen {
			return 0, fmt.Errorf("IPv4 packet should have at least %d bytes, got %d bytes", ipv4MinHeaderLen, len(p))
		}
		// Protocol is in the 10th byte of IPv4 header
		return layers.IPProtocol(p[9]), nil
	case 6:
		if len(p) < ipv6HeaderLen {
			return 0, fmt.Errorf("IPv6 packet should have at least %d bytes, got %d bytes", ipv6HeaderLen, len(p))
		}
		// Next header is in the 7th byte of IPv6 header
		return layers.IPProtocol(p[6]), nil
	default:
		return 0, fmt.Errorf("unknow ip version %d", version)
	}
}

func FindIPVersion(p []byte) (uint8, error) {
	if len(p) == 0 {
		return 0, fmt.Errorf("packet length is 0")
	}
	return p[0] >> 4, nil
}

// IPDecoder decodes raw packets into IP. It can process packets sequentially without allocating
// memory for the layers, so it cannot be called concurrently.
type IPDecoder struct {
	ipv4   *layers.IPv4
	ipv6   *layers.IPv6
	layers uint8

	v4parser *gopacket.DecodingLayerParser
	v6parser *gopacket.DecodingLayerParser
}

func NewIPDecoder() *IPDecoder {
	var (
		ipv4 layers.IPv4
		ipv6 layers.IPv6
	)
	dlpv4 := gopacket.NewDecodingLayerParser(layers.LayerTypeIPv4)
	dlpv4.SetDecodingLayerContainer(gopacket.DecodingLayerSparse(nil))
	dlpv4.AddDecodingLayer(&ipv4)
	// Stop parsing when it encounter a layer that it doesn't have a parser
	dlpv4.IgnoreUnsupported = true

	dlpv6 := gopacket.NewDecodingLayerParser(layers.LayerTypeIPv6)
	dlpv6.SetDecodingLayerContainer(gopacket.DecodingLayerSparse(nil))
	dlpv6.AddDecodingLayer(&ipv6)
	dlpv6.IgnoreUnsupported = true

	return &IPDecoder{
		ipv4:     &ipv4,
		ipv6:     &ipv6,
		layers:   1,
		v4parser: dlpv4,
		v6parser: dlpv6,
	}
}

func (pd *IPDecoder) Decode(packet RawPacket) (*IP, error) {
	// Should decode to IP layer
	decoded, err := pd.decodeByVersion(packet.Data)
	if err != nil {
		return nil, err
	}

	for _, layerType := range decoded {
		switch layerType {
		case layers.LayerTypeIPv4:
			return newIPv4(pd.ipv4)
		case layers.LayerTypeIPv6:
			return newIPv6(pd.ipv6)
		}
	}
	return nil, fmt.Errorf("no ip layer is decoded")
}

func (pd *IPDecoder) decodeByVersion(packet []byte) ([]gopacket.LayerType, error) {
	version, err := FindIPVersion(packet)
	if err != nil {
		return nil, err
	}
	decoded := make([]gopacket.LayerType, 0, pd.layers)
	switch version {
	case 4:
		err = pd.v4parser.DecodeLayers(packet, &decoded)
	case 6:
		err = pd.v6parser.DecodeLayers(packet, &decoded)
	default:
		err = fmt.Errorf("unknow ip version %d", version)
	}
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

// ICMPDecoder decodes raw packets into IP and ICMP. It can process packets sequentially without allocating
// memory for the layers, so it cannot be called concurrently.
type ICMPDecoder struct {
	*IPDecoder
	icmpv4 *layers.ICMPv4
	icmpv6 *layers.ICMPv6
}

func NewICMPDecoder() *ICMPDecoder {
	ipDecoder := NewIPDecoder()

	var (
		icmpv4 layers.ICMPv4
		icmpv6 layers.ICMPv6
	)
	ipDecoder.layers++
	ipDecoder.v4parser.AddDecodingLayer(&icmpv4)
	ipDecoder.v6parser.AddDecodingLayer(&icmpv6)

	return &ICMPDecoder{
		IPDecoder: ipDecoder,
		icmpv4:    &icmpv4,
		icmpv6:    &icmpv6,
	}
}

func (pd *ICMPDecoder) Decode(packet RawPacket) (*ICMP, error) {
	// Should decode to IP and optionally ICMP layer
	decoded, err := pd.decodeByVersion(packet.Data)
	if err != nil {
		return nil, err
	}

	for _, layerType := range decoded {
		switch layerType {
		case layers.LayerTypeICMPv4:
			ipv4, err := newIPv4(pd.ipv4)
			if err != nil {
				return nil, err
			}
			msg, err := icmp.ParseMessage(int(layers.IPProtocolICMPv4), append(pd.icmpv4.Contents, pd.icmpv4.Payload...))
			if err != nil {
				return nil, errors.Wrap(err, "failed to parse ICMPv4 message")
			}
			return &ICMP{
				IP:      ipv4,
				Message: msg,
			}, nil
		case layers.LayerTypeICMPv6:
			ipv6, err := newIPv6(pd.ipv6)
			if err != nil {
				return nil, err
			}
			msg, err := icmp.ParseMessage(int(layers.IPProtocolICMPv6), append(pd.icmpv6.Contents, pd.icmpv6.Payload...))
			if err != nil {
				return nil, errors.Wrap(err, "failed to parse ICMPv6")
			}
			return &ICMP{
				IP:      ipv6,
				Message: msg,
			}, nil
		}
	}
	layers := make([]string, len(decoded))
	for i, l := range decoded {
		layers[i] = l.String()
	}
	return nil, fmt.Errorf("Expect to decode IP and ICMP layers, got %s", layers)
}
