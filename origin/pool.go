package origin

import (
	"sync"
)

type bufferPool struct {
	// A bufferPool must not be copied after first use.
	// https://golang.org/pkg/sync/#Pool
	buffers sync.Pool
}

func newBufferPool(bufferSize int) *bufferPool {
	return &bufferPool{
		buffers: sync.Pool{
			New: func() interface{} {
				return make([]byte, bufferSize)
			},
		},
	}
}

func (p *bufferPool) Get() []byte {
	return p.buffers.Get().([]byte)
}

func (p *bufferPool) Put(buf []byte) {
	p.buffers.Put(buf)
}
