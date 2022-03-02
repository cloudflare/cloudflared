package quic

import (
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
)

type SafeStreamCloser struct {
	lock   sync.Mutex
	stream quic.Stream
}

func NewSafeStreamCloser(stream quic.Stream) *SafeStreamCloser {
	return &SafeStreamCloser{
		stream: stream,
	}
}

func (s *SafeStreamCloser) Read(p []byte) (n int, err error) {
	return s.stream.Read(p)
}

func (s *SafeStreamCloser) Write(p []byte) (n int, err error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.stream.Write(p)
}

func (s *SafeStreamCloser) Close() error {
	// Make sure a possible writer does not block the lock forever. We need it, so we can close the writer
	// side of the stream safely.
	_ = s.stream.SetWriteDeadline(time.Now())

	// This lock is eventually acquired despite Write also acquiring it, because we set a deadline to writes.
	s.lock.Lock()
	defer s.lock.Unlock()

	// We have to clean up the receiving stream ourselves since the Close in the bottom does not handle that.
	s.stream.CancelRead(0)
	return s.stream.Close()
}
