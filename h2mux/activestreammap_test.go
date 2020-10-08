package h2mux

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShutdown(t *testing.T) {
	const numStreams = 1000
	m := newActiveStreamMap(true, ActiveStreams)

	// Add all the streams
	{
		var wg sync.WaitGroup
		wg.Add(numStreams)
		for i := 0; i < numStreams; i++ {
			go func(streamID int) {
				defer wg.Done()
				stream := &MuxedStream{streamID: uint32(streamID)}
				ok := m.Set(stream)
				assert.True(t, ok)
			}(i)
		}
		wg.Wait()
	}
	assert.Equal(t, numStreams, m.Len(), "All the streams should have been added")

	shutdownChan, alreadyInProgress := m.Shutdown()
	select {
	case <-shutdownChan:
		assert.Fail(t, "before Shutdown(), shutdownChan shouldn't be closed")
	default:
	}
	assert.False(t, alreadyInProgress)

	shutdownChan2, alreadyInProgress2 := m.Shutdown()
	assert.Equal(t, shutdownChan, shutdownChan2, "repeated calls to Shutdown() should return the same channel")
	assert.True(t, alreadyInProgress2, "repeated calls to Shutdown() should return true for 'in progress'")

	// Delete all the streams
	{
		var wg sync.WaitGroup
		wg.Add(numStreams)
		for i := 0; i < numStreams; i++ {
			go func(streamID int) {
				defer wg.Done()
				m.Delete(uint32(streamID))
			}(i)
		}
		wg.Wait()
	}
	assert.Equal(t, 0, m.Len(), "All the streams should have been deleted")

	select {
	case <-shutdownChan:
	default:
		assert.Fail(t, "After all the streams are deleted, shutdownChan should have been closed")
	}
}

func TestEmptyBeforeShutdown(t *testing.T) {
	const numStreams = 1000
	m := newActiveStreamMap(true, ActiveStreams)

	// Add all the streams
	{
		var wg sync.WaitGroup
		wg.Add(numStreams)
		for i := 0; i < numStreams; i++ {
			go func(streamID int) {
				defer wg.Done()
				stream := &MuxedStream{streamID: uint32(streamID)}
				ok := m.Set(stream)
				assert.True(t, ok)
			}(i)
		}
		wg.Wait()
	}
	assert.Equal(t, numStreams, m.Len(), "All the streams should have been added")

	// Delete all the streams, bringing m to size 0
	{
		var wg sync.WaitGroup
		wg.Add(numStreams)
		for i := 0; i < numStreams; i++ {
			go func(streamID int) {
				defer wg.Done()
				m.Delete(uint32(streamID))
			}(i)
		}
		wg.Wait()
	}
	assert.Equal(t, 0, m.Len(), "All the streams should have been deleted")

	// Add one stream back
	const soloStreamID = uint32(0)
	ok := m.Set(&MuxedStream{streamID: soloStreamID})
	assert.True(t, ok)

	shutdownChan, alreadyInProgress := m.Shutdown()
	select {
	case <-shutdownChan:
		assert.Fail(t, "before Shutdown(), shutdownChan shouldn't be closed")
	default:
	}
	assert.False(t, alreadyInProgress)

	shutdownChan2, alreadyInProgress2 := m.Shutdown()
	assert.Equal(t, shutdownChan, shutdownChan2, "repeated calls to Shutdown() should return the same channel")
	assert.True(t, alreadyInProgress2, "repeated calls to Shutdown() should return true for 'in progress'")

	// Remove the remaining stream
	m.Delete(soloStreamID)

	select {
	case <-shutdownChan:
	default:
		assert.Fail(t, "After all the streams are deleted, shutdownChan should have been closed")
	}
}

type noopBuffer struct {
	isClosed bool
}

func (t *noopBuffer) Read(p []byte) (n int, err error)  { return len(p), nil }
func (t *noopBuffer) Write(p []byte) (n int, err error) { return len(p), nil }
func (t *noopBuffer) Reset()                            {}
func (t *noopBuffer) Len() int                          { return 0 }
func (t *noopBuffer) Close() error                      { t.isClosed = true; return nil }
func (t *noopBuffer) Closed() bool                      { return t.isClosed }

type noopReadyList struct{}

func (_ *noopReadyList) Signal(streamID uint32) {}

func TestAbort(t *testing.T) {
	const numStreams = 1000
	m := newActiveStreamMap(true, ActiveStreams)

	var openedStreams sync.Map

	// Add all the streams
	{
		var wg sync.WaitGroup
		wg.Add(numStreams)
		for i := 0; i < numStreams; i++ {
			go func(streamID int) {
				defer wg.Done()
				stream := &MuxedStream{
					streamID:    uint32(streamID),
					readBuffer:  &noopBuffer{},
					writeBuffer: &noopBuffer{},
					readyList:   &noopReadyList{},
				}
				ok := m.Set(stream)
				assert.True(t, ok)

				openedStreams.Store(stream.streamID, stream)
			}(i)
		}
		wg.Wait()
	}
	assert.Equal(t, numStreams, m.Len(), "All the streams should have been added")

	shutdownChan, alreadyInProgress := m.Shutdown()
	select {
	case <-shutdownChan:
		assert.Fail(t, "before Abort(), shutdownChan shouldn't be closed")
	default:
	}
	assert.False(t, alreadyInProgress)

	m.Abort()
	assert.Equal(t, numStreams, m.Len(), "Abort() shouldn't delete any streams")
	openedStreams.Range(func(key interface{}, value interface{}) bool {
		stream := value.(*MuxedStream)
		readBuffer := stream.readBuffer.(*noopBuffer)
		writeBuffer := stream.writeBuffer.(*noopBuffer)
		return assert.True(t, readBuffer.isClosed && writeBuffer.isClosed, "Abort() should have closed all the streams")
	})

	select {
	case <-shutdownChan:
	default:
		assert.Fail(t, "after Abort(), shutdownChan should have been closed")
	}

	// multiple aborts shouldn't cause any issues
	m.Abort()
	m.Abort()
	m.Abort()
}
