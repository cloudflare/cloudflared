package ackhandler

import (
	"sync"

	"github.com/quic-go/quic-go/internal/wire"
)

type Frame struct {
	wire.Frame // nil if the frame has already been acknowledged in another packet
	OnLost     func(wire.Frame)
	OnAcked    func(wire.Frame)
}

var framePool = sync.Pool{New: func() any { return &Frame{} }}

func GetFrame() *Frame {
	f := framePool.Get().(*Frame)
	f.OnLost = nil
	f.OnAcked = nil
	return f
}

func putFrame(f *Frame) {
	f.Frame = nil
	f.OnLost = nil
	f.OnAcked = nil
	framePool.Put(f)
}
