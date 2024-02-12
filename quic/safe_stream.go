package quic

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type SafeStreamCloser struct {
	lock         sync.Mutex
	stream       quic.Stream
	writeTimeout time.Duration
	log          *zerolog.Logger
}

func NewSafeStreamCloser(stream quic.Stream, writeTimeout time.Duration, log *zerolog.Logger) *SafeStreamCloser {
	return &SafeStreamCloser{
		stream:       stream,
		writeTimeout: writeTimeout,
		log:          log,
	}
}

func (s *SafeStreamCloser) Read(p []byte) (n int, err error) {
	return s.stream.Read(p)
}

func (s *SafeStreamCloser) Write(p []byte) (n int, err error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	if s.writeTimeout > 0 {
		err = s.stream.SetWriteDeadline(time.Now().Add(s.writeTimeout))
		if err != nil {
			log.Err(err).Msg("Error setting write deadline for QUIC stream")
		}
	}
	nBytes, err := s.stream.Write(p)
	if err != nil {
		s.handleTimeout(err)
	}

	return nBytes, err
}

// Handles the timeout error in case it happened, by canceling the stream write.
func (s *SafeStreamCloser) handleTimeout(err error) {
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			s.log.Error().Err(netErr).Msg("Closing quic stream due to timeout while writing")
			s.stream.CancelWrite(0)
		}
	}
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

func (s *SafeStreamCloser) CloseWrite() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	// As documented by the quic-go library, this doesn't actually close the entire stream.
	// It prevents further writes, which in turn will result in an EOF signal being sent the other side of stream when
	// reading.
	// We can still read from this stream.
	return s.stream.Close()
}

func (s *SafeStreamCloser) SetDeadline(deadline time.Time) error {
	return s.stream.SetDeadline(deadline)
}
