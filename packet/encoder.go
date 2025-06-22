package packet

import (
	"github.com/google/gopacket"
)

var (
	serializeOpts = gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}
)

// RawPacket represents a raw packet or one encoded by Encoder
type RawPacket struct {
	Data []byte
}

type Encoder struct {
	// buf is reusable because SerializeLayers calls the Clear method before each encoding
	buf gopacket.SerializeBuffer
}

func NewEncoder() *Encoder {
	return &Encoder{
		buf: gopacket.NewSerializeBuffer(),
	}
}

func (e *Encoder) Encode(packet Packet) (RawPacket, error) {
	encodedLayers, err := packet.EncodeLayers()
	if err != nil {
		return RawPacket{}, err
	}
	if err := gopacket.SerializeLayers(e.buf, serializeOpts, encodedLayers...); err != nil {
		return RawPacket{}, err
	}
	return RawPacket{
		Data: e.buf.Bytes(),
	}, nil
}
