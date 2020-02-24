package buffer

import (
	"sync"
)

type Pool struct {
	// A Pool must not be copied after first use.
	// https://golang.org/pkg/sync/#Pool
	buffers sync.Pool
}

func NewPool(bufferSize int) *Pool {
	return &Pool{
		buffers: sync.Pool{
			New: func() interface{} {
				return make([]byte, bufferSize)
			},
		},
	}
}

func (p *Pool) Get() []byte {
	return p.buffers.Get().([]byte)
}

func (p *Pool) Put(buf []byte) {
	p.buffers.Put(buf)
}
