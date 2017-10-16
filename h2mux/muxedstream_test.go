package h2mux

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const testWindowSize uint32 = 65535
const testMaxWindowSize uint32 = testWindowSize << 2

// Only sending WINDOW_UPDATE frame, so sendWindow should never change
func TestFlowControlSingleStream(t *testing.T) {
	stream := &MuxedStream{
		responseHeadersReceived: make(chan struct{}),
		readBuffer:              NewSharedBuffer(),
		receiveWindow:           testWindowSize,
		receiveWindowCurrentMax: testWindowSize,
		receiveWindowMax:        testMaxWindowSize,
		sendWindow:              testWindowSize,
		readyList:               NewReadyList(),
	}
	assert.True(t, stream.consumeReceiveWindow(testWindowSize/2))
	dataSent := testWindowSize / 2
	assert.Equal(t, testWindowSize-dataSent, stream.receiveWindow)
	assert.Equal(t, testWindowSize, stream.receiveWindowCurrentMax)
	assert.Equal(t, uint32(0), stream.windowUpdate)
	tempWindowUpdate := stream.windowUpdate

	streamChunk := stream.getChunk()
	assert.Equal(t, tempWindowUpdate, streamChunk.windowUpdate)
	assert.Equal(t, testWindowSize-dataSent, stream.receiveWindow)
	assert.Equal(t, uint32(0), stream.windowUpdate)
	assert.Equal(t, testWindowSize, stream.sendWindow)

	assert.True(t, stream.consumeReceiveWindow(2))
	dataSent += 2
	assert.Equal(t, testWindowSize-dataSent, stream.receiveWindow)
	assert.Equal(t, testWindowSize<<1, stream.receiveWindowCurrentMax)
	assert.Equal(t, (testWindowSize<<1)-stream.receiveWindow, stream.windowUpdate)
	tempWindowUpdate = stream.windowUpdate

	streamChunk = stream.getChunk()
	assert.Equal(t, tempWindowUpdate, streamChunk.windowUpdate)
	assert.Equal(t, testWindowSize<<1, stream.receiveWindow)
	assert.Equal(t, uint32(0), stream.windowUpdate)
	assert.Equal(t, testWindowSize, stream.sendWindow)

	assert.True(t, stream.consumeReceiveWindow(testWindowSize+10))
	dataSent = testWindowSize + 10
	assert.Equal(t, (testWindowSize<<1)-dataSent, stream.receiveWindow)
	assert.Equal(t, testWindowSize<<2, stream.receiveWindowCurrentMax)
	assert.Equal(t, (testWindowSize<<2)-stream.receiveWindow, stream.windowUpdate)
	tempWindowUpdate = stream.windowUpdate

	streamChunk = stream.getChunk()
	assert.Equal(t, tempWindowUpdate, streamChunk.windowUpdate)
	assert.Equal(t, testWindowSize<<2, stream.receiveWindow)
	assert.Equal(t, uint32(0), stream.windowUpdate)
	assert.Equal(t, testWindowSize, stream.sendWindow)

	assert.False(t, stream.consumeReceiveWindow(testMaxWindowSize+1))
	assert.Equal(t, testWindowSize<<2, stream.receiveWindow)
	assert.Equal(t, testMaxWindowSize, stream.receiveWindowCurrentMax)
}
