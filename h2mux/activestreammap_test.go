package h2mux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetrics(t *testing.T) {
	streamMap := newActiveStreamMap(false)
	for i := 1; i <= 7; i++ {
		stream := new(MuxedStream)
		*stream = MuxedStream{
			streamID:      defaultWindowSize * uint32(i),
			receiveWindow: defaultWindowSize * uint32(i),
			sendWindow:    defaultWindowSize * uint32(i),
		}

		assert.True(t, streamMap.Set(stream))
	}
	metrics := streamMap.Metrics()
	assert.Equal(t, float64(defaultWindowSize*4), metrics.AverageReceiveWindowSize)
	assert.Equal(t, float64(defaultWindowSize*4), metrics.AverageSendWindowSize)
	assert.Equal(t, defaultWindowSize, metrics.MinReceiveWindowSize)
	assert.Equal(t, defaultWindowSize, metrics.MinSendWindowSize)
	assert.Equal(t, defaultWindowSize*7, metrics.MaxReceiveWindowSize)
	assert.Equal(t, defaultWindowSize*7, metrics.MaxSendWindowSize)
}
