//go:build !windows

package quic

const (
	MaxDatagramFrameSize = 1350
	// maxDatagramPayloadSize is the maximum packet size allowed by warp client
	maxDatagramPayloadSize = 1280
)
