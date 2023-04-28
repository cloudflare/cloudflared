package connection

import (
	"time"

	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/h2mux"
)

const (
	muxerTimeout = 5 * time.Second
)

type MuxerConfig struct {
	HeartbeatInterval  time.Duration
	MaxHeartbeats      uint64
	CompressionSetting h2mux.CompressionSetting
	MetricsUpdateFreq  time.Duration
}

func (mc *MuxerConfig) H2MuxerConfig(h h2mux.MuxedStreamHandler, log *zerolog.Logger) *h2mux.MuxerConfig {
	return &h2mux.MuxerConfig{
		Timeout:            muxerTimeout,
		Handler:            h,
		IsClient:           true,
		HeartbeatInterval:  mc.HeartbeatInterval,
		MaxHeartbeats:      mc.MaxHeartbeats,
		Log:                log,
		CompressionQuality: mc.CompressionSetting,
	}
}
