package packet

import (
	"bytes"
	"net/netip"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func TestNewICMPTTLExceedPacket(t *testing.T) {
	ipv4Packet := IP{
		Src:      netip.MustParseAddr("192.168.1.1"),
		Dst:      netip.MustParseAddr("10.0.0.1"),
		Protocol: layers.IPProtocolICMPv4,
		TTL:      0,
	}
	icmpV4Packet := ICMP{
		IP: &ipv4Packet,
		Message: &icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   25821,
				Seq:  58129,
				Data: []byte("test ttl=0"),
			},
		},
	}
	assertTTLExceedPacket(t, &icmpV4Packet)
	icmpV4Packet.Body = &icmp.Echo{
		ID:   3487,
		Seq:  19183,
		Data: make([]byte, ipv4MinMTU),
	}
	assertTTLExceedPacket(t, &icmpV4Packet)
	ipv6Packet := IP{
		Src:      netip.MustParseAddr("fd51:2391:523:f4ee::1"),
		Dst:      netip.MustParseAddr("fd51:2391:697:f4ee::2"),
		Protocol: layers.IPProtocolICMPv6,
		TTL:      0,
	}
	icmpV6Packet := ICMP{
		IP: &ipv6Packet,
		Message: &icmp.Message{
			Type: ipv6.ICMPTypeEchoRequest,
			Code: 0,
			Body: &icmp.Echo{
				ID:   25821,
				Seq:  58129,
				Data: []byte("test ttl=0"),
			},
		},
	}
	assertTTLExceedPacket(t, &icmpV6Packet)
	icmpV6Packet.Body = &icmp.Echo{
		ID:   1497,
		Seq:  39284,
		Data: make([]byte, ipv6MinMTU),
	}
	assertTTLExceedPacket(t, &icmpV6Packet)
}

func assertTTLExceedPacket(t *testing.T, pk *ICMP) {
	encoder := NewEncoder()
	rawPacket, err := encoder.Encode(pk)
	require.NoError(t, err)

	minMTU := ipv4MinMTU
	headerLen := ipv4MinHeaderLen
	routerIP := netip.MustParseAddr("172.16.0.3")
	if pk.Dst.Is6() {
		minMTU = ipv6MinMTU
		headerLen = ipv6HeaderLen
		routerIP = netip.MustParseAddr("fd51:2391:697:f4ee::3")
	}

	ttlExceedPacket := NewICMPTTLExceedPacket(pk.IP, rawPacket, routerIP)
	require.Equal(t, routerIP, ttlExceedPacket.Src)
	require.Equal(t, pk.Src, ttlExceedPacket.Dst)
	require.Equal(t, pk.Protocol, ttlExceedPacket.Protocol)
	require.Equal(t, DefaultTTL, ttlExceedPacket.TTL)

	timeExceed, ok := ttlExceedPacket.Body.(*icmp.TimeExceeded)
	require.True(t, ok)
	if len(rawPacket.Data) > minMTU {
		require.True(t, bytes.Equal(timeExceed.Data, rawPacket.Data[:minMTU-headerLen-icmpHeaderLen]))
	} else {
		require.True(t, bytes.Equal(timeExceed.Data, rawPacket.Data))
	}

	rawTTLExceedPacket, err := encoder.Encode(ttlExceedPacket)
	require.NoError(t, err)
	if len(rawPacket.Data) > minMTU {
		require.Len(t, rawTTLExceedPacket.Data, minMTU)
	} else {
		require.Len(t, rawTTLExceedPacket.Data, headerLen+icmpHeaderLen+len(rawPacket.Data))
		require.True(t, bytes.Equal(rawPacket.Data, rawTTLExceedPacket.Data[headerLen+icmpHeaderLen:]))
	}

	decoder := NewICMPDecoder()
	decodedPacket, err := decoder.Decode(rawTTLExceedPacket)
	require.NoError(t, err)
	assertICMPChecksum(t, decodedPacket)
}

func assertICMPChecksum(t *testing.T, icmpPacket *ICMP) {
	buf := gopacket.NewSerializeBuffer()
	if icmpPacket.Protocol == layers.IPProtocolICMPv4 {
		icmpv4 := layers.ICMPv4{
			TypeCode: layers.CreateICMPv4TypeCode(uint8(icmpPacket.Type.(ipv4.ICMPType)), uint8(icmpPacket.Code)),
		}
		switch body := icmpPacket.Body.(type) {
		case *icmp.Echo:
			icmpv4.Id = uint16(body.ID)
			icmpv4.Seq = uint16(body.Seq)
			payload := gopacket.Payload(body.Data)
			require.NoError(t, payload.SerializeTo(buf, serializeOpts))
		default:
			require.NoError(t, serializeICMPAsPayload(icmpPacket.Message, buf))
		}
		// SerializeTo sets the checksum in icmpv4
		require.NoError(t, icmpv4.SerializeTo(buf, serializeOpts))
		require.Equal(t, icmpv4.Checksum, uint16(icmpPacket.Checksum))
	} else {
		switch body := icmpPacket.Body.(type) {
		case *icmp.Echo:
			payload := gopacket.Payload(body.Data)
			require.NoError(t, payload.SerializeTo(buf, serializeOpts))
			echo := layers.ICMPv6Echo{
				Identifier: uint16(body.ID),
				SeqNumber:  uint16(body.Seq),
			}
			require.NoError(t, echo.SerializeTo(buf, serializeOpts))
		default:
			require.NoError(t, serializeICMPAsPayload(icmpPacket.Message, buf))
		}

		icmpv6 := layers.ICMPv6{
			TypeCode: layers.CreateICMPv6TypeCode(uint8(icmpPacket.Type.(ipv6.ICMPType)), uint8(icmpPacket.Code)),
		}
		ipLayer := layers.IPv6{
			Version:    6,
			SrcIP:      icmpPacket.Src.AsSlice(),
			DstIP:      icmpPacket.Dst.AsSlice(),
			NextHeader: icmpPacket.Protocol,
			HopLimit:   icmpPacket.TTL,
		}
		require.NoError(t, icmpv6.SetNetworkLayerForChecksum(&ipLayer))

		// SerializeTo sets the checksum in icmpv4
		require.NoError(t, icmpv6.SerializeTo(buf, serializeOpts))
		require.Equal(t, icmpv6.Checksum, uint16(icmpPacket.Checksum))
	}
}

func serializeICMPAsPayload(message *icmp.Message, buf gopacket.SerializeBuffer) error {
	serializedBody, err := message.Body.Marshal(message.Type.Protocol())
	if err != nil {
		return err
	}
	payload := gopacket.Payload(serializedBody)
	return payload.SerializeTo(buf, serializeOpts)
}

func TestChecksum(t *testing.T) {
	data := []byte{0x63, 0x2c, 0x49, 0xd6, 0x00, 0x0d, 0xc1, 0xda}
	pk := ICMP{
		IP: &IP{
			Src:      netip.MustParseAddr("2606:4700:110:89c1:c63a:861:e08c:b049"),
			Dst:      netip.MustParseAddr("fde8:b693:d420:109b::2"),
			Protocol: layers.IPProtocolICMPv6,
			TTL:      3,
		},
		Message: &icmp.Message{
			Type: ipv6.ICMPTypeEchoRequest,
			Code: 0,
			Body: &icmp.Echo{
				ID:   0x20a7,
				Seq:  8,
				Data: data,
			},
		},
	}
	encoder := NewEncoder()
	encoded, err := encoder.Encode(&pk)
	require.NoError(t, err)

	decoder := NewICMPDecoder()
	decoded, err := decoder.Decode(encoded)
	require.Equal(t, 0xff96, decoded.Checksum)
}
