package cfio

import (
	"io"
	"sync"
)

const defaultBufferSize = 16 * 1024

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, defaultBufferSize)
	},
}

func Copy(dst io.Writer, src io.Reader) (written int64, err error) {
	_, okWriteTo := src.(io.WriterTo)
	_, okReadFrom := dst.(io.ReaderFrom)
	var buffer []byte = nil

	if !(okWriteTo || okReadFrom) {
		buffer = bufferPool.Get().([]byte)
		defer bufferPool.Put(buffer)
	}

	return io.CopyBuffer(dst, src, buffer)
}
