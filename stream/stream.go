package stream

import (
	"encoding/hex"
	"fmt"
	"io"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/cfio"
)

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

func (s *bidirectionalStreamStatus) waitAnyDone() {
	<-s.doneChan
}
func (s *bidirectionalStreamStatus) isAnyDone() bool {
	return atomic.LoadUint32(&s.anyDone) > 0
}

// Pipe copies copy data to & from provided io.ReadWriters.
func Pipe(tunnelConn, originConn io.ReadWriter, log *zerolog.Logger) {
	status := newBiStreamStatus()

	go unidirectionalStream(tunnelConn, originConn, "origin->tunnel", status, log)
	go unidirectionalStream(originConn, tunnelConn, "tunnel->origin", status, log)

	// If one side is done, we are done.
	status.waitAnyDone()
}

func unidirectionalStream(dst io.Writer, src io.Reader, dir string, status *bidirectionalStreamStatus, log *zerolog.Logger) {
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
