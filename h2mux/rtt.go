package h2mux

import (
	"sync/atomic"
)

// PingTimestamp is an atomic interface around ping timestamping and signalling.
type PingTimestamp struct {
	ts     int64
	signal Signal
}

func NewPingTimestamp() *PingTimestamp {
	return &PingTimestamp{signal: NewSignal()}
}

func (pt *PingTimestamp) Set(v int64) {
	if atomic.SwapInt64(&pt.ts, v) != 0 {
		pt.signal.Signal()
	}
}

func (pt *PingTimestamp) Get() int64 {
	return atomic.SwapInt64(&pt.ts, 0)
}

func (pt *PingTimestamp) GetUpdateChan() <-chan struct{} {
	return pt.signal.WaitChannel()
}
