package quic

import "time"

const (
	HandshakeIdleTimeout = 5 * time.Second
	MaxIdleTimeout       = 5 * time.Second
	MaxIdlePingPeriod    = 1 * time.Second

	// MaxIncomingStreams is 2^60, which is the maximum supported value by Quic-Go
	MaxIncomingStreams = 1 << 60

	// InitialPacketSizeIPv4 and InitialPacketSizeIPv6 preserve the tunnel packet
	// sizes used before quic-go 0.44 raised its default to 1280, for WARP paths
	// whose MTU is 1280.
	InitialPacketSizeIPv4 uint16 = 1232
	InitialPacketSizeIPv6 uint16 = 1252
)
