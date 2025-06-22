package tunnelrpc

import (
	"context"
	"io"
	"time"

	"github.com/pkg/errors"
	capnp "zombiezen.com/go/capnproto2"
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

// NoopCapnpLogger provides a logger to discard all capnp rpc internal logging messages as
// they are by default provided to stdout if no logger interface is provided. These logging
// messages in cloudflared have typically not provided a high amount of pratical value
// as the messages are extremely verbose and don't provide a good insight into the message
// contents or rpc method names.
type noopCapnpLogger struct{}

func (noopCapnpLogger) Infof(ctx context.Context, format string, args ...interface{})  {}
func (noopCapnpLogger) Errorf(ctx context.Context, format string, args ...interface{}) {}

func NewClientConn(transport rpc.Transport) *rpc.Conn {
	return rpc.NewConn(transport, rpc.ConnLog(noopCapnpLogger{}))
}

func NewServerConn(transport rpc.Transport, client capnp.Client) *rpc.Conn {
	return rpc.NewConn(transport, rpc.MainInterface(client), rpc.ConnLog(noopCapnpLogger{}))
}
