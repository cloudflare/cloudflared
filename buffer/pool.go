package buffer

import (
	"sync"
)

type Pool struct {
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
