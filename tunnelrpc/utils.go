package tunnelrpc

import (
	"io"
	"time"

	"github.com/pkg/errors"
	"zombiezen.com/go/capnproto2/rpc"
)

const (
	// These default values are here so that we give some time for the underlying connection/stream
	// to recover in the face of what we believe to be temporarily errors.
	// We don't want to be too aggressive, as the end result of giving a final error (non-temporary)
	// will result in the connection to be dropped.
	// In turn, the other side will probably reconnect, which will put again more pressure in the overall system.
	// So, the best solution is to give it some conservative time to recover.
	defaultSleepBetweenTemporaryError = 500 * time.Millisecond
	defaultMaxRetries                 = 3
)

type readWriterSafeTemporaryErrorCloser struct {
	io.ReadWriteCloser

	retries             int
	sleepBetweenRetries time.Duration
	maxRetries          int
}

func (r *readWriterSafeTemporaryErrorCloser) Read(p []byte) (n int, err error) {
	n, err = r.ReadWriteCloser.Read(p)

	// if there was a failure reading from the read closer, and the error is temporary, try again in some seconds
	// otherwise, just fail without a temporary error.
	if n == 0 && err != nil && isTemporaryError(err) {
		if r.retries >= r.maxRetries {
			return 0, errors.Wrap(err, "failed read from capnproto ReaderWriter after multiple temporary errors")
		} else {
			r.retries += 1

			// sleep for some time to prevent quick read loops that cause exhaustion of CPU resources
			time.Sleep(r.sleepBetweenRetries)
		}
	}

	if err == nil {
		r.retries = 0
	}

	return n, err
}

func SafeTransport(rw io.ReadWriteCloser) rpc.Transport {
	return rpc.StreamTransport(&readWriterSafeTemporaryErrorCloser{
		ReadWriteCloser:     rw,
		maxRetries:          defaultMaxRetries,
		sleepBetweenRetries: defaultSleepBetweenTemporaryError,
	})
}

// isTemporaryError reports whether e has a Temporary() method that
// returns true.
func isTemporaryError(e error) bool {
	type temp interface {
		Temporary() bool
	}
	t, ok := e.(temp)
	return ok && t.Temporary()
}
