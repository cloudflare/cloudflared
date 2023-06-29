package stream

import (
	"io"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// DebugStream will tee each read and write to the output logger as a debug message
type DebugStream struct {
	reader io.Reader
	writer io.Writer
	log    *zerolog.Logger
	max    uint64
	count  atomic.Uint64
}

func NewDebugStream(stream io.ReadWriter, logger *zerolog.Logger, max uint64) *DebugStream {
	return &DebugStream{
		reader: stream,
		writer: stream,
		log:    logger,
		max:    max,
	}
}

func (d *DebugStream) Read(p []byte) (n int, err error) {
	n, err = d.reader.Read(p)
	if n > 0 && d.max > d.count.Load() {
		d.count.Add(1)
		if err != nil {
			d.log.Err(err).
				Str("dir", "r").
				Int("count", n).
				Msgf("%+q", p[:n])
		} else {
			d.log.Debug().
				Str("dir", "r").
				Int("count", n).
				Msgf("%+q", p[:n])
		}
	}
	return
}

func (d *DebugStream) Write(p []byte) (n int, err error) {
	n, err = d.writer.Write(p)
	if n > 0 && d.max > d.count.Load() {
		d.count.Add(1)
		if err != nil {
			d.log.Err(err).
				Str("dir", "w").
				Int("count", n).
				Msgf("%+q", p[:n])
		} else {
			d.log.Debug().
				Str("dir", "w").
				Int("count", n).
				Msgf("%+q", p[:n])
		}
	}
	return
}
