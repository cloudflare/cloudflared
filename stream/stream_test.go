package stream

import (
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestPipeBidirectionalFinishBothSides(t *testing.T) {
	fun := func(upstream, downstream *mockedStream) {
		downstream.closeReader()
		upstream.closeReader()
	}

	testPipeBidirectionalUnblocking(t, fun, time.Millisecond*200, false)
}

func TestPipeBidirectionalFinishOneSideTimeout(t *testing.T) {
	fun := func(upstream, downstream *mockedStream) {
		downstream.closeReader()
	}

	testPipeBidirectionalUnblocking(t, fun, time.Millisecond*200, true)
}

func TestPipeBidirectionalClosingWriteBothSidesAlsoExists(t *testing.T) {
	fun := func(upstream, downstream *mockedStream) {
		downstream.CloseWrite()
		upstream.CloseWrite()

		downstream.writeToReader("abc")
		upstream.writeToReader("abc")
	}

	testPipeBidirectionalUnblocking(t, fun, time.Millisecond*200, false)
}

func TestPipeBidirectionalClosingWriteSingleSideAlsoExists(t *testing.T) {
	fun := func(upstream, downstream *mockedStream) {
		downstream.CloseWrite()

		downstream.writeToReader("abc")
		upstream.writeToReader("abc")
	}

	testPipeBidirectionalUnblocking(t, fun, time.Millisecond*200, true)
}

func testPipeBidirectionalUnblocking(t *testing.T, afterFun func(*mockedStream, *mockedStream), timeout time.Duration, expectTimeout bool) {
	logger := zerolog.Nop()

	downstream := newMockedStream()
	upstream := newMockedStream()

	resultCh := make(chan error)
	go func() {
		resultCh <- PipeBidirectional(downstream, upstream, timeout, &logger)
	}()

	afterFun(upstream, downstream)

	select {
	case err := <-resultCh:
		if expectTimeout {
			require.NotNil(t, err)
		} else {
			require.Nil(t, err)
		}

	case <-time.After(timeout * 2):
		require.Fail(t, "test timeout")
	}
}

func newMockedStream() *mockedStream {
	return &mockedStream{
		readCh:  make(chan *string),
		writeCh: make(chan struct{}),
	}
}

type mockedStream struct {
	readCh  chan *string
	writeCh chan struct{}

	writeCloseOnce sync.Once
}

func (m *mockedStream) Read(p []byte) (n int, err error) {
	result := <-m.readCh
	if result == nil {
		return 0, io.EOF
	}

	return len(*result), nil
}

func (m *mockedStream) Write(p []byte) (n int, err error) {
	<-m.writeCh

	return 0, fmt.Errorf("closed")
}

func (m *mockedStream) CloseWrite() error {
	m.writeCloseOnce.Do(func() {
		close(m.writeCh)
	})

	return nil
}

func (m *mockedStream) closeReader() {
	close(m.readCh)
}
func (m *mockedStream) writeToReader(content string) {
	m.readCh <- &content
}
