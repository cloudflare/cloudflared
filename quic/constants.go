package quic

import "time"

const (
	HandshakeIdleTimeout = 5 * time.Second
	MaxIdleTimeout       = 5 * time.Second
	MaxIdlePingPeriod    = 1 * time.Second

	// MaxIncomingStreams is 2^60, which is the maximum supported value by Quic-Go
	MaxIncomingStreams = 1 << 60
)
