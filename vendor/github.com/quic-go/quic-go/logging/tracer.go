package logging

import "net"

//go:generate go run generate_multiplexer.go Tracer tracer.go multiplexer.tmpl tracer_multiplexer.go

// A Tracer traces events.
type Tracer struct {
	SentPacket                   func(dest net.Addr, hdr *Header, size ByteCount, frames []Frame)
	SentVersionNegotiationPacket func(dest net.Addr, destConnID, srcConnID ArbitraryLenConnectionID, versions []Version)
	DroppedPacket                func(addr net.Addr, packetType PacketType, size ByteCount, reason PacketDropReason)
	Debug                        func(name, msg string)
	Close                        func()
}
