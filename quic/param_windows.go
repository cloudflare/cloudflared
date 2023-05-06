//go:build windows

package quic

const (
	// Due to https://github.com/quic-go/quic-go/issues/3273, MTU discovery is disabled on Windows
	// 1220 is the default value https://github.com/quic-go/quic-go/blob/84e03e59760ceee37359688871bb0688fcc4e98f/internal/protocol/params.go#L138
	MaxDatagramFrameSize = 1220
	//  3 more bytes are reserved at https://github.com/quic-go/quic-go/blob/v0.24.0/internal/wire/datagram_frame.go#L61
	maxDatagramPayloadSize = MaxDatagramFrameSize - 3 - sessionIDLen - typeIDLen
)
