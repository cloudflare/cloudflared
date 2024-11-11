package packet

import (
	"net"
	"net/netip"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func TestDecodeIP(t *testing.T) {
	ipDecoder := NewIPDecoder()
	icmpDecoder := NewICMPDecoder()
	udps := []UDP{
		{
			IP: IP{
				Src:      netip.MustParseAddr("172.16.0.1"),
				Dst:      netip.MustParseAddr("10.0.0.1"),
				Protocol: layers.IPProtocolUDP,
			},
			SrcPort: 31678,
			DstPort: 53,
		},
		{

			IP: IP{
				Src:      netip.MustParseAddr("fd51:2391:523:f4ee::1"),
				Dst:      netip.MustParseAddr("fd51:2391:697:f4ee::2"),
				Protocol: layers.IPProtocolUDP,
			},
			SrcPort: 52139,
			DstPort: 1053,
		},
	}

	encoder := NewEncoder()
	for _, udp := range udps {
		p, err := encoder.Encode(&udp)
		require.NoError(t, err)

		ipPacket, err := ipDecoder.Decode(p)
		require.NoError(t, err)
		assertIPLayer(t, &udp.IP, ipPacket)

		icmpPacket, err := icmpDecoder.Decode(p)
		require.Error(t, err)
		require.Nil(t, icmpPacket)
	}
}

func TestDecodeICMP(t *testing.T) {
	ipDecoder := NewIPDecoder()
	icmpDecoder := NewICMPDecoder()
	var (
		ipv4Packet = IP{
			Src:      netip.MustParseAddr("172.16.0.1"),
			Dst:      netip.MustParseAddr("10.0.0.1"),
			Protocol: layers.IPProtocolICMPv4,
			TTL:      DefaultTTL,
		}
		ipv6Packet = IP{
			Src:      netip.MustParseAddr("fd51:2391:523:f4ee::1"),
			Dst:      netip.MustParseAddr("fd51:2391:697:f4ee::2"),
			Protocol: layers.IPProtocolICMPv6,
			TTL:      DefaultTTL,
		}
		icmpID  = 100
		icmpSeq = 52819
	)
	tests := []struct {
		testCase string
		packet   *ICMP
	}{
		{
			testCase: "icmpv4 time exceed",
			packet: &ICMP{
				IP: &ipv4Packet,
				Message: &icmp.Message{
					Type: ipv4.ICMPTypeTimeExceeded,
					Code: 0,
					Body: &icmp.TimeExceeded{
						Data: []byte("original packet"),
					},
				},
			},
		},
		{
			testCase: "icmpv4 echo",
			packet: &ICMP{
				IP: &ipv4Packet,
				Message: &icmp.Message{
					Type: ipv4.ICMPTypeEcho,
					Code: 0,
					Body: &icmp.Echo{
						ID:   icmpID,
						Seq:  icmpSeq,
						Data: []byte("icmpv4 echo"),
					},
				},
			},
		},
		{
			testCase: "icmpv6 destination unreachable",
			packet: &ICMP{
				IP: &ipv6Packet,
				Message: &icmp.Message{
					Type: ipv6.ICMPTypeDestinationUnreachable,
					Code: 4,
					Body: &icmp.DstUnreach{
						Data: []byte("original packet"),
					},
				},
			},
		},
		{
			testCase: "icmpv6 echo",
			packet: &ICMP{
				IP: &ipv6Packet,
				Message: &icmp.Message{
					Type: ipv6.ICMPTypeEchoRequest,
					Code: 0,
					Body: &icmp.Echo{
						ID:   icmpID,
						Seq:  icmpSeq,
						Data: []byte("icmpv6 echo"),
					},
				},
			},
		},
	}

	encoder := NewEncoder()
	for _, test := range tests {
		p, err := encoder.Encode(test.packet)
		require.NoError(t, err)

		ipPacket, err := ipDecoder.Decode(p)
		require.NoError(t, err)
		if ipPacket.Src.Is4() {
			assertIPLayer(t, &ipv4Packet, ipPacket)
		} else {
			assertIPLayer(t, &ipv6Packet, ipPacket)
		}
		icmpPacket, err := icmpDecoder.Decode(p)
		require.NoError(t, err)
		require.Equal(t, ipPacket, icmpPacket.IP)

		require.Equal(t, test.packet.Type, icmpPacket.Type)
		require.Equal(t, test.packet.Code, icmpPacket.Code)
		assertICMPChecksum(t, icmpPacket)
		require.Equal(t, test.packet.Body, icmpPacket.Body)
		expectedBody, err := test.packet.Body.Marshal(test.packet.Type.Protocol())
		require.NoError(t, err)
		decodedBody, err := icmpPacket.Body.Marshal(test.packet.Type.Protocol())
		require.NoError(t, err)
		require.Equal(t, expectedBody, decodedBody)
	}
}

// TestDecodeBadPackets makes sure decoders don't decode invalid packets
func TestDecodeBadPackets(t *testing.T) {
	var (
		srcIPv4 = net.ParseIP("172.16.0.1")
		dstIPv4 = net.ParseIP("10.0.0.1")
	)

	ipLayer := layers.IPv4{
		Version:  10,
		SrcIP:    srcIPv4,
		DstIP:    dstIPv4,
		Protocol: layers.IPProtocolICMPv4,
		TTL:      DefaultTTL,
	}
	icmpLayer := layers.ICMPv4{
		TypeCode: layers.CreateICMPv4TypeCode(uint8(ipv4.ICMPTypeEcho), 0),
		Id:       100,
		Seq:      52819,
	}
	wrongIPVersion, err := createPacket(&ipLayer, &icmpLayer, nil, nil)
	require.NoError(t, err)

	tests := []struct {
		testCase string
		packet   []byte
	}{
		{
			testCase: "unknown IP version",
			packet:   wrongIPVersion,
		},
		{
			testCase: "invalid packet",
			packet:   []byte("not a packet"),
		},
		{
			testCase: "zero length packet",
			packet:   []byte{},
		},
	}

	ipDecoder := NewIPDecoder()
	icmpDecoder := NewICMPDecoder()
	for _, test := range tests {
		ipPacket, err := ipDecoder.Decode(RawPacket{Data: test.packet})
		require.Error(t, err)
		require.Nil(t, ipPacket)

		icmpPacket, err := icmpDecoder.Decode(RawPacket{Data: test.packet})
		require.Error(t, err)
		require.Nil(t, icmpPacket)
	}
}

func createPacket(ipLayer, secondLayer, thirdLayer gopacket.SerializableLayer, body []byte) ([]byte, error) {
	payload := gopacket.Payload(body)
	packet := gopacket.NewSerializeBuffer()
	var err error
	if thirdLayer != nil {
		err = gopacket.SerializeLayers(packet, serializeOpts, ipLayer, secondLayer, thirdLayer, payload)
	} else {
		err = gopacket.SerializeLayers(packet, serializeOpts, ipLayer, secondLayer, payload)
	}
	if err != nil {
		return nil, err
	}
	return packet.Bytes(), nil
}

func assertIPLayer(t *testing.T, expected, actual *IP) {
	require.Equal(t, expected.Src, actual.Src)
	require.Equal(t, expected.Dst, actual.Dst)
	require.Equal(t, expected.Protocol, actual.Protocol)
	require.Equal(t, expected.TTL, actual.TTL)
}

type UDP struct {
	IP
	SrcPort, DstPort layers.UDPPort
}

func (u *UDP) EncodeLayers() ([]gopacket.SerializableLayer, error) {
	ipLayers, err := u.IP.EncodeLayers()
	if err != nil {
		return nil, err
	}
	udpLayer := layers.UDP{
		SrcPort: u.SrcPort,
		DstPort: u.DstPort,
	}
	udpLayer.SetNetworkLayerForChecksum(ipLayers[0].(gopacket.NetworkLayer))
	return append(ipLayers, &udpLayer), nil
}

func FuzzIPDecoder(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		ipDecoder := NewIPDecoder()
		ipDecoder.Decode(RawPacket{Data: data})

	})
}

func FuzzICMPDecoder(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		icmpDecoder := NewICMPDecoder()
		icmpDecoder.Decode(RawPacket{Data: data})
	})
}
