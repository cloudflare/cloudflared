package stream

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/cfio"
)

// DefaultTimeoutAfterFirstClose controls the upper bound of how long we wait for the second stream to finish.
// We use bidirectional streams for our communication. Since the read and write sides can be closed independently,
// we must have a way to close the second stream once the first one finishes. We don't want to wait indefinitely,
// since we want to prevent misbehaving clients from blocking cloudflared.
const DefaultTimeoutAfterFirstClose = time.Second * 10

type Stream interface {
	Reader
	WriterCloser
}

type Reader interface {
	io.Reader
}

type WriterCloser interface {
	io.Writer
	WriteCloser
}

type WriteCloser interface {
	CloseWrite() error
}

type nopCloseWriterAdapter struct {
	io.ReadWriter
}

func noopCloseWriter(stream io.ReadWriter) *nopCloseWriterAdapter {
	return &nopCloseWriterAdapter{stream}
}

func (n *nopCloseWriterAdapter) CloseWrite() error {
	return nil
}

type bidirectionalStreamStatus struct {
	doneChan chan struct{}
	anyDone  uint32
}

func newBiStreamStatus() *bidirectionalStreamStatus {
	return &bidirectionalStreamStatus{
		doneChan: make(chan struct{}, 2),
		anyDone:  0,
	}
}

func (s *bidirectionalStreamStatus) markUniStreamDone() {
	atomic.StoreUint32(&s.anyDone, 1)
	s.doneChan <- struct{}{}
}

func (s *bidirectionalStreamStatus) wait(maxWaitForSecondStream time.Duration) error {
	<-s.doneChan

	// Only wait for second stream to finish if maxWait is greater than zero
	if maxWaitForSecondStream > 0 {
		timer := time.NewTimer(maxWaitForSecondStream)
		defer timer.Stop()

		select {
		case <-timer.C:
			return fmt.Errorf("timeout waiting for second stream to finish %s", maxWaitForSecondStream)
		case <-s.doneChan:
			return nil
		}
	}

	return nil
}
func (s *bidirectionalStreamStatus) isAnyDone() bool {
	return atomic.LoadUint32(&s.anyDone) > 0
}

// Pipe copies copy data to & from provided io.ReadWriters.
func Pipe(tunnelConn, originConn io.ReadWriter, timeoutAfterFirstClose time.Duration, log *zerolog.Logger) {
	if err := PipeBidirectional(noopCloseWriter(tunnelConn), noopCloseWriter(originConn), timeoutAfterFirstClose, log); err != nil {
		log.Warn().Err(err).Msg("Failed to pipe bidirectional stream")
	}
}

// PipeBidirectional copies data between two unidirectional streams. It is a special case of Pipe that accepts streams
// whose read and write sides can be closed independently. The main difference is that when piping data from a reader
// to a writer, if EOF is read, this implementation propagates the EOF signal to the destination by closing the write
// side of the bidirectional stream.
// Finally, once EOF is received from one of the provided streams, the other direction has a configured grace period to
// finish; otherwise, the method returns a timeout error. It is, however, the responsibility of the caller to close
// the associated streams at both ends in order to free all resources and goroutines.
func PipeBidirectional(downstream, upstream Stream, maxWaitForSecondStream time.Duration, log *zerolog.Logger) error {
	status := newBiStreamStatus()

	go unidirectionalStream(downstream, upstream, "upstream->downstream", status, log)
	go unidirectionalStream(upstream, downstream, "downstream->upstream", status, log)

	if err := status.wait(maxWaitForSecondStream); err != nil {
		return fmt.Errorf("unable to wait for both streams while proxying: %w", err)
	}

	return nil
}

func unidirectionalStream(dst WriterCloser, src Reader, dir string, status *bidirectionalStreamStatus, log *zerolog.Logger) {
	defer func() {
		// The bidirectional streaming spawns 2 goroutines to stream each direction.
		// If any ends, the callstack returns, meaning the Tunnel request/stream (depending on http2 vs quic) will
		// close. In such case, if the other direction did not stop (due to application level stopping, e.g., if a
		// server/origin listens forever until closure), it may read/write from the underlying ReadWriter (backed by
		// the Edge<->cloudflared transport) in an unexpected state.
		// Because of this, we set this recover() logic.
		if err := recover(); err != nil {
			if status.isAnyDone() {
				// We handle such unexpected errors only when we detect that one side of the streaming is done.
				log.Debug().Msgf("recovered from panic in stream.Pipe for %s, error %s, %s", dir, err, debug.Stack())
			} else {
				// Otherwise, this is unexpected, but we prevent the program from crashing anyway.
				log.Warn().Msgf("recovered from panic in stream.Pipe for %s, error %s, %s", dir, err, debug.Stack())
				sentry.CurrentHub().Recover(err)
				sentry.Flush(time.Second * 5)
			}
		}
	}()

	defer func() { _ = dst.CloseWrite() }()

	_, err := copyData(dst, src, dir)
	if err != nil {
		log.Debug().Msgf("%s copy: %v", dir, err)
	}
	status.markUniStreamDone()
}

// when set to true, enables logging of content copied to/from origin and tunnel
const debugCopy = false

func copyData(dst io.Writer, src io.Reader, dir string) (written int64, err error) {
	if debugCopy {
		// copyBuffer is based on stdio Copy implementation but shows copied data
		copyBuffer := func(dst io.Writer, src io.Reader, dir string) (written int64, err error) {
			var buf []byte
			size := 32 * 1024
			buf = make([]byte, size)
			for {
				t := time.Now()
				nr, er := src.Read(buf)
				if nr > 0 {
					fmt.Println(dir, t.UnixNano(), "\n"+hex.Dump(buf[0:nr]))
					nw, ew := dst.Write(buf[0:nr])
					if nw < 0 || nr < nw {
						nw = 0
						if ew == nil {
							ew = errors.New("invalid write")
						}
					}
					written += int64(nw)
					if ew != nil {
						err = ew
						break
					}
					if nr != nw {
						err = io.ErrShortWrite
						break
					}
				}
				if er != nil {
					if er != io.EOF {
						err = er
					}
					break
				}
			}
			return written, err
		}
		return copyBuffer(dst, src, dir)
	} else {
		return cfio.Copy(dst, src)
	}
}
